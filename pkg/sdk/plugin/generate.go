package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	rbacv1 "k8s.io/api/rbac/v1"
)

type pluginCRData struct {
	Name            string
	Condition       string
	FailureStrategy string
	DomainHooks     []domainHookTmplData
	NodeHooks       []nodeHookTmplData
}

type domainHookTmplData struct {
	Socket          string
	Expression      string
	Condition       string
	FailureStrategy string
	Timeout         string
}

type nodeHookTmplData struct {
	HookPoint       string
	Socket          string
	Condition       string
	FailureStrategy string
	Timeout         string
}

type hookSidecar struct {
	Image           string   `json:"image"`
	ImagePullPolicy string   `json:"imagePullPolicy,omitempty"`
	Command         []string `json:"command,omitempty"`
	Args            []string `json:"args,omitempty"`
}

const pluginCRTmplStr = `apiVersion: plugin.kubevirt.io/v1alpha1
kind: Plugin
metadata:
  name: {{ .Name }}
spec:
{{- if .Condition }}
  condition: "{{ .Condition }}"
{{- end }}
{{- if .FailureStrategy }}
  failureStrategy: {{ .FailureStrategy }}
{{- end }}
{{- if .DomainHooks }}
  domainHooks:
{{- range .DomainHooks }}
{{- if .Expression }}
  - cel:
      expression: {{ yamlStr .Expression }}
{{- else }}
  - sidecar:
      socketPath: {{ .Socket }}
{{- end }}
{{- if .Condition }}
    condition: "{{ .Condition }}"
{{- end }}
{{- if .FailureStrategy }}
    failureStrategy: {{ .FailureStrategy }}
{{- end }}
{{- if .Timeout }}
    timeout: {{ .Timeout }}
{{- end }}
{{- end }}
{{- end }}
{{- if .NodeHooks }}
  nodeHooks:
{{- range .NodeHooks }}
  - hookPoint: {{ .HookPoint }}
    socket: {{ .Socket }}
{{- if .Condition }}
    condition: "{{ .Condition }}"
{{- end }}
{{- if .FailureStrategy }}
    failureStrategy: {{ .FailureStrategy }}
{{- end }}
{{- if .Timeout }}
    timeout: {{ .Timeout }}
{{- end }}
{{- end }}
{{- end }}
`

const daemonSetTmplStr = `apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: {{ .Name }}
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
{{- if .Args }}
        args:
{{- range .Args }}
        - "{{ . }}"
{{- end }}
{{- end }}
        volumeMounts:
        - name: plugin-socket
          mountPath: /var/run/kubevirt/plugins
      volumes:
      - name: plugin-socket
        hostPath:
          path: /var/run/kubevirt/plugins
          type: DirectoryOrCreate
`

const rbacTmplStr = `apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ .Name }}
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

const mapTmplStr = `apiVersion: admissionregistration.k8s.io/v1alpha1
kind: MutatingAdmissionPolicy
metadata:
  name: {{ .Name }}
spec:
  matchConstraints:
    resourceRules:
    - apiGroups: ["kubevirt.io"]
      apiVersions: ["*"]
      resources: ["virtualmachineinstances"]
      operations: ["CREATE"]
  paramKind:
    apiVersion: plugin.kubevirt.io/v1alpha1
    kind: Plugin
  reinvocationPolicy: IfNeeded
  mutations:
  - patchType: ApplyConfiguration
    applyConfiguration:
      expression: |-
        Object{
          metadata: Object.metadata{
            annotations: {
              "hooks.kubevirt.io/hookSidecars": {{ .SidecarCEL }}
            }
          }
        }
`

const mapBindingTmplStr = `apiVersion: admissionregistration.k8s.io/v1alpha1
kind: MutatingAdmissionPolicyBinding
metadata:
  name: {{ .Name }}
spec:
  policyName: {{ .PolicyName }}
  paramRef:
    name: {{ .ParamRefName }}
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

const makefileTmplStr = `REGISTRY ?= quay.io/myorg
TAG ?= latest
IMAGE = $(REGISTRY)/{{.Name}}:$(TAG)

CONTAINER_ENGINE ?= $(shell command -v podman 2>/dev/null || echo docker)

.PHONY: build push

build:
	$(CONTAINER_ENGINE) build -t $(IMAGE) .

push: build
	$(CONTAINER_ENGINE) push $(IMAGE)
`

func celStringLiteral(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

func (p *Plugin) WithRBACRules(rules []rbacv1.PolicyRule) *Plugin {
	p.rbacRules = rules
	return p
}

func (p *Plugin) Generate(outputDir string) error {
	return p.generate(outputDir, ".")
}

func (p *Plugin) generate(outputDir, sourceDir string) error {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	hasNode := len(p.nodeHooks) > 0
	hasDomain := len(p.domainHooks) > 0
	sidecarDomainHooks := p.sidecarDomainHooks()
	hasSidecarDomain := len(sidecarDomainHooks) > 0

	if !hasNode && !hasDomain {
		return fmt.Errorf("no hooks registered; nothing to generate")
	}

	nodeEntrypoints := p.uniqueNodeEntrypoints()

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
			suffix := "daemonset.yaml"
			if entrypoint != p.name {
				suffix = entrypoint + "-daemonset.yaml"
			}
			yamlFiles = append(yamlFiles, artifact{suffix, content})
		}
	}

	content, err := p.renderPluginCR()
	if err != nil {
		return err
	}
	yamlFiles = append(yamlFiles, artifact{"plugin.yaml", content})

	if hasSidecarDomain {
		content, err := p.renderMAP()
		if err != nil {
			return err
		}
		yamlFiles = append(yamlFiles, artifact{"mutating-admission-policy.yaml", content})

		content, err = p.renderMAPBinding()
		if err != nil {
			return err
		}
		yamlFiles = append(yamlFiles, artifact{"mutating-admission-policy-binding.yaml", content})
	}

	for i, file := range yamlFiles {
		filename := fmt.Sprintf("%02d-%s", i+1, file.suffix)
		if err := os.WriteFile(filepath.Join(outputDir, filename), []byte(file.content), 0644); err != nil {
			return fmt.Errorf("write %s: %w", filename, err)
		}
	}

	hasServableHooks := hasSidecarDomain || hasNode
	if hasServableHooks {
		goVersion, err := readGoVersion(sourceDir)
		if err != nil {
			return err
		}
		dockerfileContent, err := renderTemplate(dockerfileTmplStr, struct{ GoVersion string }{goVersion})
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(sourceDir, "Dockerfile"), []byte(dockerfileContent), 0644); err != nil {
			return fmt.Errorf("write Dockerfile: %w", err)
		}

		makefileContent, err := renderTemplate(makefileTmplStr, struct{ Name string }{p.name})
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(sourceDir, "Makefile"), []byte(makefileContent), 0644); err != nil {
			return fmt.Errorf("write Makefile: %w", err)
		}
	}

	return nil
}

func (p *Plugin) renderPluginCR() (string, error) {
	data := pluginCRData{Name: p.name}

	if p.condition != "" {
		data.Condition = p.condition
	}
	if p.failureStrategy != "" {
		data.FailureStrategy = string(p.failureStrategy)
	}

	// Process domain hooks in declaration order, preserving interleaving
	renderedSidecarEntrypoints := map[string]bool{}
	for _, hook := range p.domainHooks {
		if hook.isCEL() {
			hookData := domainHookTmplData{
				Expression: hook.expression,
			}
			if hook.condition != "" {
				hookData.Condition = hook.condition
			}
			if hook.failureStrategy != nil {
				hookData.FailureStrategy = string(*hook.failureStrategy)
			}
			if hook.timeout != nil {
				hookData.Timeout = hook.timeout.String()
			}
			data.DomainHooks = append(data.DomainHooks, hookData)
		} else {
			entrypoint := p.resolveEntrypoint(hook.entrypoint)
			if renderedSidecarEntrypoints[entrypoint] {
				continue
			}
			renderedSidecarEntrypoints[entrypoint] = true

			hooks := p.domainHooksForEntrypoint(entrypoint)
			if err := validateDomainHookConsistency(entrypoint, hooks); err != nil {
				return "", err
			}
			first := hooks[0]

			hookData := domainHookTmplData{
				Socket: DomainSocketPathForEntrypoint(p.name, entrypoint),
			}
			if first.condition != "" {
				hookData.Condition = first.condition
			}
			if first.failureStrategy != nil {
				hookData.FailureStrategy = string(*first.failureStrategy)
			}
			if first.timeout != nil {
				hookData.Timeout = first.timeout.String()
			}
			data.DomainHooks = append(data.DomainHooks, hookData)
		}
	}

	for _, nodeHook := range p.nodeHooks {
		entrypoint := p.resolveEntrypoint(nodeHook.entrypoint)
		nodeHookData := nodeHookTmplData{
			HookPoint: nodeHook.hookPoint,
			Socket:    NodeSocketPathForEntrypoint(p.name, entrypoint),
		}
		if nodeHook.condition != "" {
			nodeHookData.Condition = nodeHook.condition
		}
		if nodeHook.failureStrategy != nil {
			nodeHookData.FailureStrategy = string(*nodeHook.failureStrategy)
		}
		if nodeHook.timeout != nil {
			nodeHookData.Timeout = nodeHook.timeout.String()
		}
		data.NodeHooks = append(data.NodeHooks, nodeHookData)
	}

	return renderTemplate(pluginCRTmplStr, data)
}

func (p *Plugin) renderDaemonSet(entrypoint string) (string, error) {
	name := p.name
	if entrypoint != p.name {
		name = p.name + "-" + entrypoint
	}

	image := p.resolveImage()

	data := struct {
		Name               string
		Image              string
		ImagePullPolicy    string
		ServiceAccountName string
		Args               []string
	}{
		Name:  name,
		Image: image,
	}
	data.ImagePullPolicy = p.imagePullPolicy

	if len(p.rbacRules) > 0 {
		data.ServiceAccountName = p.name
	}
	if entrypoint != p.name {
		data.Args = []string{"--entrypoint", entrypoint}
	}

	return renderTemplate(daemonSetTmplStr, data)
}

func (p *Plugin) renderRBAC() (string, error) {
	namespace := p.namespace
	if namespace == "" {
		namespace = "default"
	}
	data := struct {
		Name      string
		Namespace string
		Rules     []rbacv1.PolicyRule
	}{
		Name:      p.name,
		Namespace: namespace,
		Rules:     p.rbacRules,
	}
	return renderTemplate(rbacTmplStr, data)
}

func (p *Plugin) renderMAP() (string, error) {
	var sidecars []hookSidecar
	for _, entrypoint := range p.uniqueDomainEntrypoints() {
		sidecar := hookSidecar{Image: p.resolveImage()}
		if p.imagePullPolicy != "" {
			sidecar.ImagePullPolicy = p.imagePullPolicy
		}
		if entrypoint != p.name {
			sidecar.Args = []string{"--entrypoint", entrypoint}
		}
		sidecars = append(sidecars, sidecar)
	}

	sidecarJSON, err := json.Marshal(sidecars)
	if err != nil {
		return "", fmt.Errorf("marshal sidecar config: %w", err)
	}

	data := struct {
		Name       string
		SidecarCEL string
	}{
		Name:       p.name,
		SidecarCEL: celStringLiteral(string(sidecarJSON)),
	}

	return renderTemplate(mapTmplStr, data)
}

func (p *Plugin) renderMAPBinding() (string, error) {
	data := struct {
		Name         string
		PolicyName   string
		ParamRefName string
	}{
		Name:         p.name,
		PolicyName:   p.name,
		ParamRefName: p.name,
	}

	return renderTemplate(mapBindingTmplStr, data)
}

func collectUniqueEntrypoints[T any](hooks []T, getEntrypoint func(T) string) []string {
	seen := map[string]bool{}
	var entrypoints []string

	for _, hook := range hooks {
		entrypoint := getEntrypoint(hook)
		if !seen[entrypoint] {
			seen[entrypoint] = true
			entrypoints = append(entrypoints, entrypoint)
		}
	}

	return entrypoints
}

func (p *Plugin) uniqueNodeEntrypoints() []string {
	return collectUniqueEntrypoints(p.nodeHooks, func(hook NodeHookOption) string {
		return p.resolveEntrypoint(hook.entrypoint)
	})
}

func (p *Plugin) uniqueDomainEntrypoints() []string {
	return collectUniqueEntrypoints(p.sidecarDomainHooks(), func(hook DomainHookOption) string {
		return p.resolveEntrypoint(hook.entrypoint)
	})
}

func (p *Plugin) sidecarDomainHooks() []DomainHookOption {
	var result []DomainHookOption
	for _, h := range p.domainHooks {
		if !h.isCEL() {
			result = append(result, h)
		}
	}
	return result
}

func readGoVersion(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("read go.mod: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "go ") {
			return strings.TrimPrefix(line, "go "), nil
		}
	}
	return "", fmt.Errorf("go version not found in go.mod")
}

func validateDomainHookConsistency(entrypoint string, hooks []DomainHookOption) error {
	if len(hooks) <= 1 {
		return nil
	}

	first := hooks[0]
	for i := 1; i < len(hooks); i++ {
		hook := hooks[i]

		if hook.condition != first.condition {
			return fmt.Errorf("domain hooks sharing entrypoint %q have conflicting conditions: %q vs %q",
				entrypoint, first.condition, hook.condition)
		}

		firstFailureStrategy := first.failureStrategy
		hookFailureStrategy := hook.failureStrategy
		if (firstFailureStrategy == nil) != (hookFailureStrategy == nil) || (firstFailureStrategy != nil && hookFailureStrategy != nil && *firstFailureStrategy != *hookFailureStrategy) {
			return fmt.Errorf("domain hooks sharing entrypoint %q have conflicting failure strategies", entrypoint)
		}

		firstTimeout := first.timeout
		hookTimeout := hook.timeout
		if (firstTimeout == nil) != (hookTimeout == nil) || (firstTimeout != nil && hookTimeout != nil && *firstTimeout != *hookTimeout) {
			return fmt.Errorf("domain hooks sharing entrypoint %q have conflicting timeouts", entrypoint)
		}
	}

	return nil
}

func renderTemplate(tmplStr string, data any) (string, error) {
	funcMap := template.FuncMap{
		"yamlList": func(items []string) string {
			quoted := make([]string, len(items))
			for i, s := range items {
				quoted[i] = `"` + s + `"`
			}
			return "[" + strings.Join(quoted, ", ") + "]"
		},
		"yamlStr": func(s string) string {
			data, _ := json.Marshal(s)
			return string(data)
		},
	}

	t, err := template.New("").Funcs(funcMap).Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}

	return buf.String(), nil
}
