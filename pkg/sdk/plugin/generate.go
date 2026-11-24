package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/version"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/template"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	pluginapi "kubevirt.io/api/plugin"
	pluginv1alpha1 "kubevirt.io/api/plugin/v1alpha1"
	"sigs.k8s.io/yaml"
)

const daemonSetTmplStr = `apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: {{ .Name }}
  namespace: {{ .Namespace }}
  labels:
    app: {{ .Name }}
spec:
  selector:
    matchLabels:
      app: {{ .Name }}
  template:
    metadata:
      labels:
        app: {{ .Name }}
    spec:
{{- if .ServiceAccountName }}
      serviceAccountName: {{ .ServiceAccountName }}
{{- end }}
      containers:
      - name: {{ .Name }}
        image: {{ .Image }}
{{- if .ImagePullPolicy }}
        imagePullPolicy: {{ .ImagePullPolicy }}
{{- end }}
        securityContext:
          privileged: true
          runAsUser: 0
{{- if .Args }}
        args:
{{- range .Args }}
        - "{{ . }}"
{{- end }}
{{- end }}
        volumeMounts:
        - name: plugin-socket
          mountPath: {{ .NodeSocketBasePath }}
      volumes:
      - name: plugin-socket
        hostPath:
          path: {{ .NodeSocketBasePath }}
          type: DirectoryOrCreate
`

const rbacTmplStr = `apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ .Name }}
  namespace: {{ .Namespace }}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: {{ .Name }}
rules:
{{- range .Rules }}
- apiGroups: {{ yamlList .APIGroups }}
  resources: {{ yamlList .Resources }}
  verbs: {{ yamlList .Verbs }}
{{- if .ResourceNames }}
  resourceNames: {{ yamlList .ResourceNames }}
{{- end }}
{{- if .NonResourceURLs }}
  nonResourceURLs: {{ yamlList .NonResourceURLs }}
{{- end }}
{{- end }}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: {{ .Name }}
subjects:
- kind: ServiceAccount
  name: {{ .Name }}
  namespace: {{ .Namespace }}
roleRef:
  kind: ClusterRole
  name: {{ .Name }}
  apiGroup: rbac.authorization.k8s.io
`

const mapTmplStr = `apiVersion: admissionregistration.k8s.io/v1
kind: MutatingAdmissionPolicy
metadata:
  name: {{ .Name }}
spec:
  failurePolicy: Fail
  reinvocationPolicy: Never
  matchConstraints:
    resourceRules:
    - apiGroups: [""]
      apiVersions: ["v1"]
      resources: ["pods"]
      operations: ["CREATE"]
  matchConditions:
  - name: is-virt-launcher-pod
    expression: 'has(object.metadata.labels) && "kubevirt.io" in object.metadata.labels && object.metadata.labels["kubevirt.io"] == "virt-launcher"'
  - name: has-plugin-socket-volume
    expression: 'object.spec.volumes.exists(v, v.name == "{{ .PluginSocketsVolumeName }}")'
{{- range .LabelConditions }}
  - name: {{ .Name }}
    expression: '{{ .Expression }}'
{{- end }}
  mutations:
  - patchType: JSONPatch
    jsonPatch:
      expression: {{ .JSONPatchExpression }}
`

const mapBindingTmplStr = `apiVersion: admissionregistration.k8s.io/v1
kind: MutatingAdmissionPolicyBinding
metadata:
  name: {{ .Name }}
spec:
  policyName: {{ .Name }}
`

const dockerfileTmplStr = `FROM golang:{{ .GoVersion }} AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o plugin .

FROM gcr.io/distroless/static
COPY --from=builder /app/plugin /
ENTRYPOINT ["/plugin", "serve"]
`

const makefileTmplStr = `IMAGE = {{ .Image }}

CONTAINER_ENGINE ?= $(shell command -v podman 2>/dev/null || echo docker)
GO_VERSION ?= {{ .GoVersion }}

.PHONY: build push test vet

build:
	$(CONTAINER_ENGINE) build -t $(IMAGE) .

push: build
	$(CONTAINER_ENGINE) push $(IMAGE)

test:
	$(CONTAINER_ENGINE) run --rm -v $(PWD):/app:Z -v {{.Name}}-modcache:/go/pkg/mod:Z -v {{.Name}}-cache:/root/.cache/go-build:Z -w /app golang:$(GO_VERSION) go test ./...

vet:
	$(CONTAINER_ENGINE) run --rm -v $(PWD):/app:Z -v {{.Name}}-modcache:/go/pkg/mod:Z -v {{.Name}}-cache:/root/.cache/go-build:Z -w /app golang:$(GO_VERSION) go vet ./...
`

func (p *Plugin) Generate(outputDir string) error {
	return p.generate(outputDir, ".")
}

func (p *Plugin) generate(outputDir, sourceDir string) error {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	hasNode := len(p.nodeHooks) > 0
	hasLauncher := len(p.launcherHooks) > 0
	hasSidecarLauncher := len(p.sidecarLauncherHooks()) > 0

	if !hasNode && !hasLauncher {
		return fmt.Errorf("no hooks registered; nothing to generate")
	}

	nodeEntrypoints := p.uniqueNodeEntrypoints()
	sidecarEntrypoints := p.uniqueSidecarEntrypoints()

	type artifact struct {
		suffix  string
		content string
	}

	var yamlFiles []artifact

	if hasNode {
		if len(p.rbacRules) > 0 {
			content, err := p.renderRBAC()
			if err != nil {
				return err
			}
			yamlFiles = append(yamlFiles, artifact{"rbac.yaml", content})
		}
		for _, entrypoint := range nodeEntrypoints {
			content, err := p.renderDaemonSet(entrypoint)
			if err != nil {
				return err
			}
			yamlFiles = append(yamlFiles, artifact{p.fileSuffix(entrypoint, "daemonset.yaml"), content})
		}
	}

	content, err := p.renderPluginCR()
	if err != nil {
		return err
	}
	yamlFiles = append(yamlFiles, artifact{"plugin.yaml", content})

	if hasSidecarLauncher {
		for _, entrypoint := range sidecarEntrypoints {
			content, err = p.renderMAP(entrypoint)
			if err != nil {
				return err
			}
			yamlFiles = append(yamlFiles, artifact{p.fileSuffix(entrypoint, "mutating-admission-policy.yaml"), content})

			content, err = p.renderMAPBinding(entrypoint)
			if err != nil {
				return err
			}
			yamlFiles = append(yamlFiles, artifact{p.fileSuffix(entrypoint, "mutating-admission-policy-binding.yaml"), content})
		}
	}

	for i, file := range yamlFiles {
		filename := fmt.Sprintf("%02d-%s", i+1, file.suffix)
		if err := os.WriteFile(filepath.Join(outputDir, filename), []byte(file.content), 0644); err != nil {
			return fmt.Errorf("write %s: %w", filename, err)
		}
	}

	hasServableHooks := hasSidecarLauncher || hasNode
	if hasServableHooks {
		goVersion, err := readGoVersion(sourceDir)
		if err != nil {
			return err
		}
		goVersion = builderGoVersion(goVersion)
		dockerfileContent, err := renderTemplate(dockerfileTmplStr, struct{ GoVersion string }{goVersion})
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(sourceDir, "Dockerfile"), []byte(dockerfileContent), 0644); err != nil {
			return fmt.Errorf("write Dockerfile: %w", err)
		}

		makefileContent, err := renderTemplate(makefileTmplStr, struct {
			Name      string
			Image     string
			GoVersion string
		}{p.name, p.resolveImage(), goVersion})
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(sourceDir, "Makefile"), []byte(makefileContent), 0644); err != nil {
			return fmt.Errorf("write Makefile: %w", err)
		}
	}

	return nil
}

// fileSuffix returns the generated-file suffix for one entrypoint's
// artifacts: the default entrypoint keeps base, other entrypoints prefix it
// so their files do not collide.
func (p *Plugin) fileSuffix(entrypoint, base string) string {
	if entrypoint == p.name {
		return base
	}
	return entrypoint + "-" + base
}

func (p *Plugin) renderPluginCR() (string, error) {
	cr := &pluginv1alpha1.Plugin{
		TypeMeta:   metav1.TypeMeta{APIVersion: pluginv1alpha1.SchemeGroupVersion.String(), Kind: pluginapi.Kind},
		ObjectMeta: metav1.ObjectMeta{Name: p.name},
	}

	if p.condition != "" {
		cr.Spec.Condition = p.condition
	}
	if p.failureStrategy != "" {
		cr.Spec.FailureStrategy = p.failureStrategy
	}

	for _, launcherHook := range p.launcherHooks {
		hook := pluginv1alpha1.LauncherHook{Condition: launcherHook.condition}
		if launcherHook.failureStrategy != nil {
			hook.FailureStrategy = *launcherHook.failureStrategy
		}
		if launcherHook.isCEL() {
			hook.CEL = &pluginv1alpha1.CELLauncherHook{HookPoint: launcherHook.hookPoint, Expression: launcherHook.expression}
		} else {
			hook.Sidecar = &pluginv1alpha1.SidecarLauncherHook{
				SocketPath:     LauncherSocketPath(p.name, p.resolveEntrypoint(launcherHook.entrypoint)),
				PermittedHooks: []pluginv1alpha1.LauncherHookPoint{launcherHook.hookPoint},
			}
			if launcherHook.timeout != nil {
				hook.Timeout = &metav1.Duration{Duration: *launcherHook.timeout}
			}
		}
		cr.Spec.LauncherHooks = append(cr.Spec.LauncherHooks, hook)
	}

	for _, nodeHook := range p.nodeHooks {
		hook := pluginv1alpha1.NodeHook{
			Socket:         NodeSocketPath(p.name, p.resolveEntrypoint(nodeHook.entrypoint)),
			PermittedHooks: []pluginv1alpha1.NodeHookPoint{nodeHook.hookPoint},
			Condition:      nodeHook.condition,
		}
		if nodeHook.failureStrategy != nil {
			hook.FailureStrategy = *nodeHook.failureStrategy
		}
		if nodeHook.timeout != nil {
			hook.Timeout = &metav1.Duration{Duration: *nodeHook.timeout}
		}
		cr.Spec.NodeHooks = append(cr.Spec.NodeHooks, hook)
	}

	for _, entrypoint := range p.uniqueSidecarEntrypoints() {
		cr.Spec.MutatingAdmissionPolicies = append(cr.Spec.MutatingAdmissionPolicies, pluginv1alpha1.AdmissionReference{Name: p.entrypointResourceName(entrypoint)})
	}

	return marshalPluginCR(cr)
}

// marshalPluginCR renders cr as YAML without the fields the typed
// marshalling adds but the API server owns: creationTimestamp and status.
func marshalPluginCR(cr *pluginv1alpha1.Plugin) (string, error) {
	data, err := json.Marshal(cr)
	if err != nil {
		return "", fmt.Errorf("marshal plugin CR: %w", err)
	}

	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return "", fmt.Errorf("unmarshal plugin CR: %w", err)
	}
	if metadata, ok := obj["metadata"].(map[string]any); ok {
		delete(metadata, "creationTimestamp")
	}
	delete(obj, "status")

	yamlData, err := yaml.Marshal(obj)
	if err != nil {
		return "", fmt.Errorf("render plugin CR YAML: %w", err)
	}
	return string(yamlData), nil
}

func (p *Plugin) renderDaemonSet(entrypoint string) (string, error) {
	data := struct {
		Name               string
		Namespace          string
		Image              string
		ImagePullPolicy    string
		ServiceAccountName string
		Args               []string
		NodeSocketBasePath string
	}{
		Name:               p.entrypointResourceName(entrypoint),
		Namespace:          p.namespace,
		Image:              p.resolveImage(),
		ImagePullPolicy:    p.imagePullPolicy,
		Args:               []string{"--entrypoint", entrypoint, "--role", roleDaemonSet},
		NodeSocketBasePath: nodeSocketBasePath,
	}

	if len(p.rbacRules) > 0 {
		data.ServiceAccountName = p.name
	}

	return renderTemplate(daemonSetTmplStr, data)
}

func (p *Plugin) renderRBAC() (string, error) {
	data := struct {
		Name      string
		Namespace string
		Rules     []rbacv1.PolicyRule
	}{
		Name:      p.name,
		Namespace: p.namespace,
		Rules:     p.rbacRules,
	}
	return renderTemplate(rbacTmplStr, data)
}

func (p *Plugin) renderMAP(entrypoint string) (string, error) {
	jsonPatchExpr := fmt.Sprintf(`'[
    JSONPatch{
        op: "add",
        path: "/spec/containers/-",
        value: Object.spec.containers{
            name: %q,
            image: %q,%s
            args: %s,%s
            securityContext: Object.spec.containers.securityContext{
                allowPrivilegeEscalation: false,
                runAsNonRoot: true,
                runAsUser: %d,
                runAsGroup: %d,
                seccompProfile: Object.spec.containers.securityContext.seccompProfile{
                    type: "RuntimeDefault"
                },
                capabilities: Object.spec.containers.securityContext.capabilities{
                    drop: ["ALL"]
                }
            },
            volumeMounts: %s
        }
    }
]'`, p.sidecarContainerName(entrypoint), p.resolveImage(), imagePullPolicyCEL(p.imagePullPolicy), quotedStringList([]string{"--entrypoint", entrypoint, "--role", roleSidecar}), resourcesCEL(p.sidecarResources), sidecarUID, sidecarGID, p.volumeMountsCEL(entrypoint))

	data := struct {
		Name                    string
		PluginSocketsVolumeName string
		LabelConditions         []labelCondition
		JSONPatchExpression     string
	}{
		Name:                    p.entrypointResourceName(entrypoint),
		PluginSocketsVolumeName: pluginSocketsVolumeName,
		LabelConditions:         p.labelConditions(),
		JSONPatchExpression:     jsonPatchExpr,
	}

	return renderTemplate(mapTmplStr, data)
}

// labelCondition is one generated matchCondition restricting the sidecar
// injection to pods carrying a target label.
type labelCondition struct {
	Name       string
	Expression string
}

// labelConditions renders the sidecar pod labels as match conditions, in
// sorted key order so the output is deterministic.
func (p *Plugin) labelConditions() []labelCondition {
	if len(p.sidecarPodLabels) == 0 {
		return nil
	}

	keys := slices.Sorted(maps.Keys(p.sidecarPodLabels))
	conditions := make([]labelCondition, 0, len(keys))
	for i, key := range keys {
		conditions = append(conditions, labelCondition{
			Name:       fmt.Sprintf("has-target-label-%d", i),
			Expression: fmt.Sprintf(`has(object.metadata.labels) && %q in object.metadata.labels && object.metadata.labels[%q] == %q`, key, key, p.sidecarPodLabels[key]),
		})
	}
	return conditions
}

func imagePullPolicyCEL(policy string) string {
	if policy == "" {
		return ""
	}
	return fmt.Sprintf("\n            imagePullPolicy: %q,", policy)
}

// resourcesCEL renders the sidecar resources as a CEL struct literal, with
// resource names sorted so the output is deterministic; an empty
// ResourceRequirements renders nothing.
func resourcesCEL(resources corev1.ResourceRequirements) string {
	sections := []string{}
	if len(resources.Requests) > 0 {
		sections = append(sections, fmt.Sprintf("requests: %s", celResourceList(resources.Requests)))
	}
	if len(resources.Limits) > 0 {
		sections = append(sections, fmt.Sprintf("limits: %s", celResourceList(resources.Limits)))
	}
	if len(sections) == 0 {
		return ""
	}
	return fmt.Sprintf("\n            resources: Object.spec.containers.resources{\n                %s\n            },", strings.Join(sections, ",\n                "))
}

func celResourceList(list corev1.ResourceList) string {
	names := slices.Sorted(maps.Keys(list))
	pairs := make([]string, 0, len(names))
	for _, name := range names {
		quantity := list[name]
		pairs = append(pairs, fmt.Sprintf("%q: %q", name, quantity.String()))
	}
	return "{" + strings.Join(pairs, ", ") + "}"
}

func (p *Plugin) volumeMountsCEL(entrypoint string) string {
	mounts := []string{fmt.Sprintf(`Object.spec.containers.volumeMounts{
                name: %q,
                mountPath: "/var/run/kubevirt-plugin/%s/",
                subPath: %q
            }`, pluginSocketsVolumeName, p.name, p.name)}
	if p.entrypointServesPreBoot(entrypoint) {
		mounts = append(mounts, fmt.Sprintf(`Object.spec.containers.volumeMounts{
                name: %q,
                mountPath: %q
            }`, ephemeralDisksVolumeName, ephemeralDisksMountPath))
	}
	return "[" + strings.Join(mounts, ", ") + "]"
}

func (p *Plugin) entrypointServesPreBoot(entrypoint string) bool {
	for _, launcherHook := range p.launcherHooks {
		if !launcherHook.isCEL() && launcherHook.hookPoint == LauncherHookPreBoot && p.resolveEntrypoint(launcherHook.entrypoint) == entrypoint {
			return true
		}
	}
	return false
}

// quotedStringList renders items as a bracketed, double-quoted list.
// strconv.Quote keeps both the CEL expressions and the YAML flow sequences
// it feeds valid for quote-containing strings.
func quotedStringList(items []string) string {
	quoted := make([]string, len(items))
	for i, item := range items {
		quoted[i] = strconv.Quote(item)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func (p *Plugin) renderMAPBinding(entrypoint string) (string, error) {
	return renderTemplate(mapBindingTmplStr, struct{ Name string }{p.entrypointResourceName(entrypoint)})
}

func appendUnique(list []string, s string) []string {
	if !slices.Contains(list, s) {
		list = append(list, s)
	}
	return list
}

func (p *Plugin) uniqueNodeEntrypoints() []string {
	var entrypoints []string
	for _, hook := range p.nodeHooks {
		entrypoints = appendUnique(entrypoints, p.resolveEntrypoint(hook.entrypoint))
	}
	return entrypoints
}

func (p *Plugin) uniqueSidecarEntrypoints() []string {
	var entrypoints []string
	for _, hook := range p.sidecarLauncherHooks() {
		entrypoints = appendUnique(entrypoints, p.resolveEntrypoint(hook.entrypoint))
	}
	return entrypoints
}

func (p *Plugin) sidecarLauncherHooks() []LauncherHookOption {
	var result []LauncherHookOption
	for _, launcherHook := range p.launcherHooks {
		if !launcherHook.isCEL() {
			result = append(result, launcherHook)
		}
	}
	return result
}

// minBuilderGoVersion is the oldest Go release generated build files use,
// matching the toolchain this SDK is built and tested with.
const minBuilderGoVersion = "1.26"

// builderGoVersion returns the golang image tag for building a plugin whose
// go.mod declares goDirective: the latest patch release of its Go minor, but
// no older than minBuilderGoVersion.
func builderGoVersion(goDirective string) string {
	lang := version.Lang("go" + goDirective)
	if version.Compare(lang, "go"+minBuilderGoVersion) < 0 {
		return minBuilderGoVersion
	}
	return strings.TrimPrefix(lang, "go")
}

func readGoVersion(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("read go.mod: %w", err)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if goDirective, ok := strings.CutPrefix(strings.TrimSpace(line), "go "); ok {
			return goDirective, nil
		}
	}
	return "", fmt.Errorf("go version not found in go.mod")
}

func renderTemplate(tmplStr string, data any) (string, error) {
	t, err := template.New("").Funcs(template.FuncMap{"yamlList": quotedStringList}).Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}

	return buf.String(), nil
}
