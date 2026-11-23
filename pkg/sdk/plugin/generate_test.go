package plugin

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	celgo "github.com/google/cel-go/cel"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	pluginv1alpha1 "kubevirt.io/api/plugin/v1alpha1"
	sigsyaml "sigs.k8s.io/yaml"
)

// Test-local types for YAML verification

type testDaemonSet struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name      string            `yaml:"name"`
		Namespace string            `yaml:"namespace"`
		Labels    map[string]string `yaml:"labels"`
	} `yaml:"metadata"`
	Spec struct {
		Selector struct {
			MatchLabels map[string]string `yaml:"matchLabels"`
		} `yaml:"selector"`
		Template struct {
			Metadata struct {
				Labels map[string]string `yaml:"labels"`
			} `yaml:"metadata"`
			Spec struct {
				ServiceAccountName string `yaml:"serviceAccountName,omitempty"`
				Containers         []struct {
					Name         string   `yaml:"name"`
					Image        string   `yaml:"image"`
					Args         []string `yaml:"args,omitempty"`
					VolumeMounts []struct {
						Name      string `yaml:"name"`
						MountPath string `yaml:"mountPath"`
					} `yaml:"volumeMounts"`
				} `yaml:"containers"`
				Volumes []struct {
					Name     string `yaml:"name"`
					HostPath struct {
						Path string `yaml:"path"`
						Type string `yaml:"type"`
					} `yaml:"hostPath"`
				} `yaml:"volumes"`
			} `yaml:"spec"`
		} `yaml:"template"`
	} `yaml:"spec"`
}

type testK8sResource struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name      string `yaml:"name"`
		Namespace string `yaml:"namespace"`
	} `yaml:"metadata"`
}

type testClusterRole struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Rules []struct {
		APIGroups       []string `yaml:"apiGroups"`
		Resources       []string `yaml:"resources"`
		ResourceNames   []string `yaml:"resourceNames"`
		NonResourceURLs []string `yaml:"nonResourceURLs"`
		Verbs           []string `yaml:"verbs"`
	} `yaml:"rules"`
}

type testClusterRoleBinding struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Subjects []struct {
		Kind      string `yaml:"kind"`
		Name      string `yaml:"name"`
		Namespace string `yaml:"namespace"`
	} `yaml:"subjects"`
	RoleRef struct {
		Kind     string `yaml:"kind"`
		Name     string `yaml:"name"`
		APIGroup string `yaml:"apiGroup"`
	} `yaml:"roleRef"`
}

type testMAP struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		FailurePolicy      string `yaml:"failurePolicy"`
		ReinvocationPolicy string `yaml:"reinvocationPolicy"`
		MatchConstraints   struct {
			ResourceRules []struct {
				APIGroups   []string `yaml:"apiGroups"`
				APIVersions []string `yaml:"apiVersions"`
				Resources   []string `yaml:"resources"`
				Operations  []string `yaml:"operations"`
			} `yaml:"resourceRules"`
		} `yaml:"matchConstraints"`
		MatchConditions []struct {
			Name       string `yaml:"name"`
			Expression string `yaml:"expression"`
		} `yaml:"matchConditions"`
		Mutations []struct {
			PatchType string `yaml:"patchType"`
			JSONPatch struct {
				Expression string `yaml:"expression"`
			} `yaml:"jsonPatch"`
		} `yaml:"mutations"`
	} `yaml:"spec"`
}

type testMAPBinding struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		PolicyName string `yaml:"policyName"`
	} `yaml:"spec"`
}

// Helpers

func setupSourceDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.23.0\n"), 0644); err != nil {
		t.Fatal(err)
	}

	return dir
}

// generatePlugin runs the generator for p and returns the source and output
// directories it used.
func generatePlugin(t *testing.T, p *Plugin) (sourceDir, outputDir string) {
	t.Helper()

	sourceDir = setupSourceDir(t)
	outputDir = filepath.Join(t.TempDir(), "deploy")
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

	return sourceDir, outputDir
}

func readAndUnmarshal(t *testing.T, path string, v any) {
	t.Helper()

	data := readFileContent(t, path)
	if err := yaml.Unmarshal([]byte(data), v); err != nil {
		t.Fatalf("failed to unmarshal %s: %v\nContent:\n%s", path, err, data)
	}
}

func readPluginCR(t *testing.T, dir string) pluginv1alpha1.Plugin {
	t.Helper()

	data := readFileContent(t, findGeneratedFile(t, dir, "plugin.yaml"))

	var cr pluginv1alpha1.Plugin
	if err := sigsyaml.Unmarshal([]byte(data), &cr); err != nil {
		t.Fatalf("failed to unmarshal plugin.yaml: %v\nContent:\n%s", err, data)
	}

	return cr
}

// assertSocketPathCRDRules checks the CRD validation rules for a sidecar
// launcher hook socket path.
func assertSocketPathCRDRules(t *testing.T, socketPath, pluginName string) {
	t.Helper()

	if !strings.HasPrefix(socketPath, "/var/run/kubevirt-plugin/"+pluginName+"/") {
		t.Fatalf("socketPath %q must start with /var/run/kubevirt-plugin/%s/", socketPath, pluginName)
	}

	if !strings.HasSuffix(socketPath, ".sock") {
		t.Fatalf("socketPath %q must end with .sock", socketPath)
	}

	if len(socketPath) > 107 {
		t.Fatalf("socketPath %q must be no longer than 107 bytes", socketPath)
	}

	if strings.Contains(socketPath, "..") || strings.Contains(socketPath, "//") || strings.HasSuffix(socketPath, "/") {
		t.Fatalf("socketPath %q must be a clean path", socketPath)
	}
}

func readFileContent(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}

	return string(data)
}

func findGeneratedFile(t *testing.T, dir, suffix string) string {
	t.Helper()

	path, ok := generatedFile(dir, suffix)
	if !ok {
		t.Fatalf("no file with suffix %q found in %s (files: %v)", suffix, dir, listFileNames(dir))
	}

	return path
}

func generatedFileExists(dir, suffix string) bool {
	_, ok := generatedFile(dir, suffix)
	return ok
}

func generatedFile(dir, suffix string) (string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}

	for _, e := range entries {
		if strings.HasSuffix(e.Name(), suffix) {
			return filepath.Join(dir, e.Name()), true
		}
	}

	return "", false
}

func listFileNames(dir string) []string {
	entries, _ := os.ReadDir(dir)

	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}

	return names
}

func splitYAMLDocuments(content string) []string {
	parts := strings.Split(content, "---\n")

	var docs []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			docs = append(docs, trimmed)
		}
	}

	return docs
}

func mustParseCEL(t *testing.T, expression string) {
	t.Helper()

	env, err := celgo.NewEnv()
	if err != nil {
		t.Fatalf("failed to create CEL environment: %v", err)
	}

	if _, issues := env.Parse(expression); issues.Err() != nil {
		t.Fatalf("expected a valid CEL expression: %v\n%s", issues.Err(), expression)
	}
}

// Tests

func TestGeneratePluginCRNodeHookOnly(t *testing.T) {
	p := New("test-plugin").WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{}))
	_, outputDir := generatePlugin(t, p)

	cr := readPluginCR(t, outputDir)

	if cr.APIVersion != "plugin.kubevirt.io/v1alpha1" {
		t.Fatalf("expected apiVersion plugin.kubevirt.io/v1alpha1, got %q", cr.APIVersion)
	}

	if cr.Kind != "Plugin" {
		t.Fatalf("expected kind Plugin, got %q", cr.Kind)
	}

	if cr.Name != "test-plugin" {
		t.Fatalf("expected name test-plugin, got %q", cr.Name)
	}

	if len(cr.Spec.NodeHooks) != 1 {
		t.Fatalf("expected 1 nodeHook, got %d", len(cr.Spec.NodeHooks))
	}

	if len(cr.Spec.NodeHooks[0].PermittedHooks) != 1 || cr.Spec.NodeHooks[0].PermittedHooks[0] != pluginv1alpha1.NodeHookPreVMStart {
		t.Fatalf("expected permittedHooks [%q], got %v", pluginv1alpha1.NodeHookPreVMStart, cr.Spec.NodeHooks[0].PermittedHooks)
	}

	if cr.Spec.NodeHooks[0].Socket != "/var/run/kubevirt/plugins/test-plugin/node.sock" {
		t.Fatalf("expected socket /var/run/kubevirt/plugins/test-plugin/node.sock, got %q", cr.Spec.NodeHooks[0].Socket)
	}

	if len(cr.Spec.LauncherHooks) != 0 {
		t.Fatal("expected no launcherHooks")
	}
}

func TestGeneratePluginCRLauncherHookOnly(t *testing.T) {
	p := New("test-plugin").WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{}))
	_, outputDir := generatePlugin(t, p)

	cr := readPluginCR(t, outputDir)

	if len(cr.Spec.LauncherHooks) != 1 {
		t.Fatalf("expected 1 launcher hook, got %d", len(cr.Spec.LauncherHooks))
	}

	hook := cr.Spec.LauncherHooks[0]
	if hook.CEL != nil {
		t.Fatal("expected a sidecar hook")
	}

	if hook.Sidecar == nil || hook.Sidecar.SocketPath != "/var/run/kubevirt-plugin/test-plugin/launcher.sock" {
		t.Fatalf("expected sidecar socket path, got %+v", hook.Sidecar)
	}

	if !slices.Contains(hook.Sidecar.PermittedHooks, pluginv1alpha1.LauncherHookGuestDefinition) {
		t.Fatalf("expected permittedHooks to contain GuestDefinition, got %v", hook.Sidecar.PermittedHooks)
	}

	assertSocketPathCRDRules(t, hook.Sidecar.SocketPath, "test-plugin")

	if len(cr.Spec.NodeHooks) != 0 {
		t.Fatal("expected no nodeHooks")
	}
}

func TestGeneratePluginCRBothHookKinds(t *testing.T) {
	p := New("test-plugin").
		WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{})).
		WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{}))
	_, outputDir := generatePlugin(t, p)

	cr := readPluginCR(t, outputDir)

	if len(cr.Spec.LauncherHooks) != 1 {
		t.Fatalf("expected 1 launcher hook, got %d", len(cr.Spec.LauncherHooks))
	}

	if len(cr.Spec.NodeHooks) != 1 {
		t.Fatalf("expected 1 nodeHook, got %d", len(cr.Spec.NodeHooks))
	}
}

func TestGeneratePluginCRWithCondition(t *testing.T) {
	p := New("test-plugin").
		WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{})).
		WithCondition("vmi.Name == 'test'")
	_, outputDir := generatePlugin(t, p)

	cr := readPluginCR(t, outputDir)

	if cr.Spec.Condition != "vmi.Name == 'test'" {
		t.Fatalf("expected condition, got %q", cr.Spec.Condition)
	}
}

func TestGeneratePluginCRWithPerHookSettings(t *testing.T) {
	timeout := 30 * time.Second
	p := New("test-plugin").WithNodeHook(NodeHookPreVMStart,
		NodeHandler(&stubNodeHandler{}).
			WithCondition("vmi.Name == 'test'").
			WithFailureStrategy(Ignore).
			WithTimeout(timeout),
	)
	_, outputDir := generatePlugin(t, p)

	cr := readPluginCR(t, outputDir)

	if len(cr.Spec.NodeHooks) != 1 {
		t.Fatalf("expected 1 nodeHook, got %d", len(cr.Spec.NodeHooks))
	}

	nodeHook := cr.Spec.NodeHooks[0]

	if nodeHook.Condition != "vmi.Name == 'test'" {
		t.Fatalf("expected per-hook condition, got %q", nodeHook.Condition)
	}

	if nodeHook.FailureStrategy != pluginv1alpha1.FailureStrategyIgnore {
		t.Fatalf("expected failureStrategy Ignore, got %q", nodeHook.FailureStrategy)
	}

	if nodeHook.Timeout == nil || nodeHook.Timeout.Duration != timeout {
		t.Fatalf("expected timeout %v, got %v", timeout, nodeHook.Timeout)
	}
}

func TestGeneratePluginCRLauncherHookPerHookSettings(t *testing.T) {
	timeout := 45 * time.Second
	p := New("test-plugin").WithLauncherHook(
		GuestDefinition(&stubGuestDefinitionHandler{}).
			WithCondition("vmi.Name == 'test'").
			WithFailureStrategy(Ignore).
			WithTimeout(timeout),
	)
	_, outputDir := generatePlugin(t, p)

	cr := readPluginCR(t, outputDir)

	if len(cr.Spec.LauncherHooks) != 1 {
		t.Fatalf("expected 1 launcher hook, got %d", len(cr.Spec.LauncherHooks))
	}

	hook := cr.Spec.LauncherHooks[0]

	if hook.Condition != "vmi.Name == 'test'" {
		t.Fatalf("expected per-hook condition, got %q", hook.Condition)
	}

	if hook.FailureStrategy != pluginv1alpha1.FailureStrategyIgnore {
		t.Fatalf("expected failureStrategy Ignore, got %q", hook.FailureStrategy)
	}

	if hook.Timeout == nil || hook.Timeout.Duration != timeout {
		t.Fatalf("expected timeout %v, got %v", timeout, hook.Timeout)
	}
}

func TestGeneratePluginCRRendersOnVMStop(t *testing.T) {
	p := New("test-plugin").WithNodeHook(NodeHookOnVMStop, NodeHandler(&stubNodeHandler{}))
	_, outputDir := generatePlugin(t, p)

	cr := readPluginCR(t, outputDir)

	if len(cr.Spec.NodeHooks) != 1 {
		t.Fatalf("expected 1 nodeHook, got %d", len(cr.Spec.NodeHooks))
	}

	if len(cr.Spec.NodeHooks[0].PermittedHooks) != 1 || cr.Spec.NodeHooks[0].PermittedHooks[0] != pluginv1alpha1.NodeHookOnVMStop {
		t.Fatalf("expected permittedHooks [%q], got %v", pluginv1alpha1.NodeHookOnVMStop, cr.Spec.NodeHooks[0].PermittedHooks)
	}
}

func TestGeneratePluginCRKeepsPluginFailureStrategyAtPluginLevel(t *testing.T) {
	p := New("test-plugin").
		WithFailureStrategy(Ignore).
		WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{})).
		WithNodeHook(NodeHookPostVMStop, NodeHandler(&stubNodeHandler{}).WithFailureStrategy(Fail))
	_, outputDir := generatePlugin(t, p)

	cr := readPluginCR(t, outputDir)

	if len(cr.Spec.NodeHooks) != 2 {
		t.Fatalf("expected 2 nodeHooks, got %d", len(cr.Spec.NodeHooks))
	}

	if cr.Spec.FailureStrategy != pluginv1alpha1.FailureStrategyIgnore {
		t.Fatalf("expected plugin-level failureStrategy Ignore, got %q", cr.Spec.FailureStrategy)
	}

	if cr.Spec.NodeHooks[0].FailureStrategy != "" {
		t.Fatalf("expected no failureStrategy on the node hook without an explicit strategy, got %q", cr.Spec.NodeHooks[0].FailureStrategy)
	}

	if cr.Spec.NodeHooks[1].FailureStrategy != pluginv1alpha1.FailureStrategyFail {
		t.Fatalf("expected explicit node hook failureStrategy Fail to be kept, got %q", cr.Spec.NodeHooks[1].FailureStrategy)
	}
}

func TestGeneratePluginCRKeepsPluginFailureStrategyOutOfLauncherHooks(t *testing.T) {
	p := New("test-plugin").
		WithFailureStrategy(Ignore).
		WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{}))
	_, outputDir := generatePlugin(t, p)

	cr := readPluginCR(t, outputDir)

	if len(cr.Spec.LauncherHooks) != 1 {
		t.Fatalf("expected 1 launcher hook, got %d", len(cr.Spec.LauncherHooks))
	}

	if cr.Spec.FailureStrategy != pluginv1alpha1.FailureStrategyIgnore {
		t.Fatalf("expected plugin-level failureStrategy Ignore, got %q", cr.Spec.FailureStrategy)
	}

	if cr.Spec.LauncherHooks[0].FailureStrategy != "" {
		t.Fatalf("expected the launcher hook entry to stay empty, got %q", cr.Spec.LauncherHooks[0].FailureStrategy)
	}
}

func TestGeneratePluginCRHasNoCreationTimestampOrStatus(t *testing.T) {
	p := New("test-plugin").WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{}))
	_, outputDir := generatePlugin(t, p)

	content := readFileContent(t, findGeneratedFile(t, outputDir, "plugin.yaml"))

	if strings.Contains(content, "creationTimestamp") {
		t.Fatalf("expected no creationTimestamp in plugin.yaml, got:\n%s", content)
	}

	if strings.Contains(content, "status") {
		t.Fatalf("expected no status in plugin.yaml, got:\n%s", content)
	}
}

func TestGeneratePluginCRLauncherHooksInDeclarationOrder(t *testing.T) {
	p := New("test-plugin").
		WithLauncherHook(GuestDefinitionCEL("Domain{Name: 'cel-first'}")).
		WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{})).
		WithLauncherHook(PreBoot(&stubPreBootHandler{})).
		WithLauncherHook(GuestDefinitionCEL("Domain{Name: 'cel-last'}"))
	_, outputDir := generatePlugin(t, p)

	cr := readPluginCR(t, outputDir)

	if len(cr.Spec.LauncherHooks) != 4 {
		t.Fatalf("expected 4 launcher hooks, got %d", len(cr.Spec.LauncherHooks))
	}

	if cr.Spec.LauncherHooks[0].CEL == nil || cr.Spec.LauncherHooks[0].CEL.Expression != "Domain{Name: 'cel-first'}" {
		t.Fatalf("expected first hook to be the first CEL hook, got %+v", cr.Spec.LauncherHooks[0])
	}

	if cr.Spec.LauncherHooks[1].Sidecar == nil || !slices.Contains(cr.Spec.LauncherHooks[1].Sidecar.PermittedHooks, pluginv1alpha1.LauncherHookGuestDefinition) {
		t.Fatalf("expected second hook to be the GuestDefinition sidecar, got %+v", cr.Spec.LauncherHooks[1])
	}

	if cr.Spec.LauncherHooks[2].Sidecar == nil || !slices.Contains(cr.Spec.LauncherHooks[2].Sidecar.PermittedHooks, pluginv1alpha1.LauncherHookPreBoot) {
		t.Fatalf("expected third hook to be the PreBoot sidecar, got %+v", cr.Spec.LauncherHooks[2])
	}

	if cr.Spec.LauncherHooks[3].CEL == nil || cr.Spec.LauncherHooks[3].CEL.Expression != "Domain{Name: 'cel-last'}" {
		t.Fatalf("expected fourth hook to be the last CEL hook, got %+v", cr.Spec.LauncherHooks[3])
	}
}

func TestGeneratePluginCRCELHook(t *testing.T) {
	p := New("test-plugin").WithLauncherHook(GuestDefinitionCEL("Domain{Name: 'test'}"))
	_, outputDir := generatePlugin(t, p)

	cr := readPluginCR(t, outputDir)

	if len(cr.Spec.LauncherHooks) != 1 || cr.Spec.LauncherHooks[0].CEL == nil {
		t.Fatal("expected 1 CEL launcher hook")
	}

	if cr.Spec.LauncherHooks[0].CEL.HookPoint != pluginv1alpha1.LauncherHookGuestDefinition {
		t.Fatalf("expected CEL hookPoint GuestDefinition, got %q", cr.Spec.LauncherHooks[0].CEL.HookPoint)
	}

	if cr.Spec.LauncherHooks[0].CEL.Expression != "Domain{Name: 'test'}" {
		t.Fatalf("expected expression, got %q", cr.Spec.LauncherHooks[0].CEL.Expression)
	}

	if cr.Spec.LauncherHooks[0].Sidecar != nil {
		t.Fatal("expected no sidecar")
	}

	if cr.Spec.LauncherHooks[0].Timeout != nil {
		t.Fatal("expected no timeout on a CEL hook")
	}

	if len(cr.Spec.MutatingAdmissionPolicies) != 0 {
		t.Fatalf("expected no admission references without sidecars, got %v", cr.Spec.MutatingAdmissionPolicies)
	}
}

func TestGeneratePluginCRSharedSocketPerEntrypoint(t *testing.T) {
	timeout := 30 * time.Second
	p := New("test-plugin").
		WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{}).WithEntrypoint("ep-a")).
		WithLauncherHook(PreBoot(&stubPreBootHandler{}).WithEntrypoint("ep-a").WithTimeout(timeout))
	_, outputDir := generatePlugin(t, p)

	cr := readPluginCR(t, outputDir)

	if len(cr.Spec.LauncherHooks) != 2 {
		t.Fatalf("expected one entry per registered hook, got %d", len(cr.Spec.LauncherHooks))
	}

	if cr.Spec.LauncherHooks[0].Sidecar.SocketPath != cr.Spec.LauncherHooks[1].Sidecar.SocketPath {
		t.Fatalf("expected hooks sharing an entrypoint to share a socket, got %q and %q", cr.Spec.LauncherHooks[0].Sidecar.SocketPath, cr.Spec.LauncherHooks[1].Sidecar.SocketPath)
	}

	if cr.Spec.LauncherHooks[0].Timeout != nil {
		t.Fatal("expected no timeout on the hook without one")
	}

	if cr.Spec.LauncherHooks[1].Timeout == nil || cr.Spec.LauncherHooks[1].Timeout.Duration != timeout {
		t.Fatalf("expected the per-hook timeout to be kept, got %v", cr.Spec.LauncherHooks[1].Timeout)
	}
}

func TestGeneratePluginCRMutatingAdmissionPolicies(t *testing.T) {
	p := New("test-plugin").
		WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{})).
		WithLauncherHook(PreBoot(&stubPreBootHandler{}).WithEntrypoint("ep-a")).
		WithLauncherHook(GuestDefinitionCEL("Domain{Name: 'test'}"))
	_, outputDir := generatePlugin(t, p)

	cr := readPluginCR(t, outputDir)

	if len(cr.Spec.MutatingAdmissionPolicies) != 2 {
		t.Fatalf("expected 2 admission references, got %d", len(cr.Spec.MutatingAdmissionPolicies))
	}

	if cr.Spec.MutatingAdmissionPolicies[0].Name != "test-plugin" {
		t.Fatalf("expected reference to the default MAP, got %q", cr.Spec.MutatingAdmissionPolicies[0].Name)
	}

	if cr.Spec.MutatingAdmissionPolicies[1].Name != "test-plugin-ep-a" {
		t.Fatalf("expected reference to the ep-a MAP, got %q", cr.Spec.MutatingAdmissionPolicies[1].Name)
	}
}

func TestGenerateDaemonSet(t *testing.T) {
	p := New("test-plugin").WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{}))
	_, outputDir := generatePlugin(t, p)

	var ds testDaemonSet
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "daemonset.yaml"), &ds)

	if ds.APIVersion != "apps/v1" {
		t.Fatalf("expected apiVersion apps/v1, got %q", ds.APIVersion)
	}

	if ds.Kind != "DaemonSet" {
		t.Fatalf("expected kind DaemonSet, got %q", ds.Kind)
	}

	if ds.Metadata.Name != "test-plugin" {
		t.Fatalf("expected name test-plugin, got %q", ds.Metadata.Name)
	}

	if ds.Metadata.Labels["app"] != "test-plugin" {
		t.Fatalf("expected label app=test-plugin, got %v", ds.Metadata.Labels)
	}

	if ds.Metadata.Namespace != "default" {
		t.Fatalf("expected namespace default, got %q", ds.Metadata.Namespace)
	}

	if len(ds.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(ds.Spec.Template.Spec.Containers))
	}

	container := ds.Spec.Template.Spec.Containers[0]

	if container.Name != "test-plugin" {
		t.Fatalf("expected container name test-plugin, got %q", container.Name)
	}

	if !strings.Contains(container.Image, "test-plugin") {
		t.Fatalf("expected image to contain plugin name, got %q", container.Image)
	}

	if len(container.VolumeMounts) == 0 {
		t.Fatal("expected volume mounts")
	}

	if container.VolumeMounts[0].MountPath != "/var/run/kubevirt/plugins" {
		t.Fatalf("expected mountPath /var/run/kubevirt/plugins, got %q", container.VolumeMounts[0].MountPath)
	}

	if len(ds.Spec.Template.Spec.Volumes) == 0 {
		t.Fatal("expected volumes")
	}

	if ds.Spec.Template.Spec.Volumes[0].HostPath.Path != "/var/run/kubevirt/plugins" {
		t.Fatalf("expected hostPath /var/run/kubevirt/plugins, got %q", ds.Spec.Template.Spec.Volumes[0].HostPath.Path)
	}
}

func TestGenerateRBACWhenRulesProvided(t *testing.T) {
	rules := []rbacv1.PolicyRule{{
		APIGroups: []string{""},
		Resources: []string{"pods"},
		Verbs:     []string{"get", "list"},
	}}

	p := New("test-plugin").
		WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{})).
		WithRBACRules(rules)
	_, outputDir := generatePlugin(t, p)

	rbacFile := findGeneratedFile(t, outputDir, "rbac.yaml")
	content := readFileContent(t, rbacFile)
	docs := splitYAMLDocuments(content)
	if len(docs) != 3 {
		t.Fatalf("expected 3 YAML documents in rbac.yaml, got %d", len(docs))
	}

	var sa testK8sResource
	if err := yaml.Unmarshal([]byte(docs[0]), &sa); err != nil {
		t.Fatalf("unmarshal ServiceAccount: %v", err)
	}

	if sa.Kind != "ServiceAccount" {
		t.Fatalf("expected ServiceAccount, got %q", sa.Kind)
	}

	if sa.Metadata.Name != "test-plugin" {
		t.Fatalf("expected SA name test-plugin, got %q", sa.Metadata.Name)
	}

	if sa.Metadata.Namespace != "default" {
		t.Fatalf("expected SA namespace default, got %q", sa.Metadata.Namespace)
	}

	var cr testClusterRole
	if err := yaml.Unmarshal([]byte(docs[1]), &cr); err != nil {
		t.Fatalf("unmarshal ClusterRole: %v", err)
	}

	if cr.Kind != "ClusterRole" {
		t.Fatalf("expected ClusterRole, got %q", cr.Kind)
	}

	if len(cr.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(cr.Rules))
	}

	if cr.Rules[0].Resources[0] != "pods" {
		t.Fatalf("expected resource pods, got %v", cr.Rules[0].Resources)
	}

	var crb testClusterRoleBinding
	if err := yaml.Unmarshal([]byte(docs[2]), &crb); err != nil {
		t.Fatalf("unmarshal ClusterRoleBinding: %v", err)
	}

	if crb.Kind != "ClusterRoleBinding" {
		t.Fatalf("expected ClusterRoleBinding, got %q", crb.Kind)
	}

	if crb.RoleRef.Name != "test-plugin" {
		t.Fatalf("expected roleRef name test-plugin, got %q", crb.RoleRef.Name)
	}

	if len(crb.Subjects) != 1 || crb.Subjects[0].Name != "test-plugin" {
		t.Fatalf("expected subject name test-plugin, got %v", crb.Subjects)
	}

	if crb.Subjects[0].Namespace != "default" {
		t.Fatalf("expected default namespace, got %q", crb.Subjects[0].Namespace)
	}
}

func TestGenerateRBACWithCustomNamespace(t *testing.T) {
	rules := []rbacv1.PolicyRule{{
		APIGroups: []string{""},
		Resources: []string{"pods"},
		Verbs:     []string{"get", "list"},
	}}

	p := New("test-plugin").
		WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{})).
		WithRBACRules(rules).
		WithNamespace("kube-system")
	_, outputDir := generatePlugin(t, p)

	rbacFile := findGeneratedFile(t, outputDir, "rbac.yaml")
	content := readFileContent(t, rbacFile)
	docs := splitYAMLDocuments(content)
	if len(docs) != 3 {
		t.Fatalf("expected 3 YAML documents in rbac.yaml, got %d", len(docs))
	}

	var crb testClusterRoleBinding
	if err := yaml.Unmarshal([]byte(docs[2]), &crb); err != nil {
		t.Fatalf("unmarshal ClusterRoleBinding: %v", err)
	}

	if crb.Subjects[0].Namespace != "kube-system" {
		t.Fatalf("expected namespace kube-system, got %q", crb.Subjects[0].Namespace)
	}

	var sa testK8sResource
	if err := yaml.Unmarshal([]byte(docs[0]), &sa); err != nil {
		t.Fatalf("unmarshal ServiceAccount: %v", err)
	}

	if sa.Metadata.Namespace != "kube-system" {
		t.Fatalf("expected SA namespace kube-system, got %q", sa.Metadata.Namespace)
	}

	var ds testDaemonSet
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "daemonset.yaml"), &ds)

	if ds.Metadata.Namespace != "kube-system" {
		t.Fatalf("expected DaemonSet namespace kube-system, got %q", ds.Metadata.Namespace)
	}
}

func TestGenerateRBACWithResourceNamesAndNonResourceURLs(t *testing.T) {
	tests := []struct {
		name string
		rule rbacv1.PolicyRule
	}{
		{"resource names", rbacv1.PolicyRule{
			APIGroups:     []string{""},
			Resources:     []string{"configmaps"},
			ResourceNames: []string{"my-config"},
			Verbs:         []string{"get"},
		}},
		{"non-resource URLs", rbacv1.PolicyRule{
			NonResourceURLs: []string{"/healthz", "/metrics"},
			Verbs:           []string{"get"},
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := New("test-plugin").
				WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{})).
				WithRBACRules([]rbacv1.PolicyRule{test.rule})
			_, outputDir := generatePlugin(t, p)

			docs := splitYAMLDocuments(readFileContent(t, findGeneratedFile(t, outputDir, "rbac.yaml")))
			if len(docs) != 3 {
				t.Fatalf("expected 3 YAML documents in rbac.yaml, got %d", len(docs))
			}

			var cr testClusterRole
			if err := yaml.Unmarshal([]byte(docs[1]), &cr); err != nil {
				t.Fatalf("unmarshal ClusterRole: %v", err)
			}

			if len(cr.Rules) != 1 {
				t.Fatalf("expected 1 rule, got %d", len(cr.Rules))
			}

			if !slices.Equal(cr.Rules[0].ResourceNames, test.rule.ResourceNames) {
				t.Fatalf("expected resourceNames %v, got %v", test.rule.ResourceNames, cr.Rules[0].ResourceNames)
			}

			if !slices.Equal(cr.Rules[0].NonResourceURLs, test.rule.NonResourceURLs) {
				t.Fatalf("expected nonResourceURLs %v, got %v", test.rule.NonResourceURLs, cr.Rules[0].NonResourceURLs)
			}
		})
	}
}

func TestGenerateNoRBACWhenNoRules(t *testing.T) {
	p := New("test-plugin").WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{}))
	_, outputDir := generatePlugin(t, p)

	if generatedFileExists(outputDir, "rbac.yaml") {
		t.Fatal("expected no rbac.yaml when no RBAC rules provided")
	}
}

func TestGenerateNoDaemonSetForLauncherOnlyPlugin(t *testing.T) {
	rules := []rbacv1.PolicyRule{{
		APIGroups: []string{""},
		Resources: []string{"pods"},
		Verbs:     []string{"get", "list"},
	}}

	p := New("test-plugin").
		WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{})).
		WithRBACRules(rules)
	_, outputDir := generatePlugin(t, p)

	if generatedFileExists(outputDir, "daemonset.yaml") {
		t.Fatalf("expected no daemonset.yaml for a launcher-only plugin, files: %v", listFileNames(outputDir))
	}

	// The RBAC rules only ever reach the node-hook DaemonSet service
	// account, so WithRBACRules is ignored without node hooks.
	if generatedFileExists(outputDir, "rbac.yaml") {
		t.Fatalf("expected no rbac.yaml for a launcher-only plugin even with RBAC rules, files: %v", listFileNames(outputDir))
	}

	expectedYAML := []string{
		"01-plugin.yaml",
		"02-mutating-admission-policy.yaml",
		"03-mutating-admission-policy-binding.yaml",
	}

	files := listFileNames(outputDir)
	if len(files) != len(expectedYAML) {
		t.Fatalf("expected %d YAML files in output dir, got %d: %v", len(expectedYAML), len(files), files)
	}

	for i, name := range expectedYAML {
		if files[i] != name {
			t.Fatalf("file %d: expected %q, got %q (all files: %v)", i, name, files[i], files)
		}
	}
}

func TestGenerateMAP(t *testing.T) {
	p := New("test-plugin").WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{}))
	_, outputDir := generatePlugin(t, p)

	var m testMAP
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "mutating-admission-policy.yaml"), &m)

	if m.APIVersion != "admissionregistration.k8s.io/v1" {
		t.Fatalf("expected apiVersion admissionregistration.k8s.io/v1, got %q", m.APIVersion)
	}

	if m.Kind != "MutatingAdmissionPolicy" {
		t.Fatalf("expected kind MutatingAdmissionPolicy, got %q", m.Kind)
	}

	if m.Metadata.Name != "test-plugin" {
		t.Fatalf("expected name test-plugin, got %q", m.Metadata.Name)
	}

	if m.Spec.FailurePolicy != "Fail" {
		t.Fatalf("expected failurePolicy Fail, got %q", m.Spec.FailurePolicy)
	}

	if m.Spec.ReinvocationPolicy != "Never" {
		t.Fatalf("expected reinvocationPolicy Never, got %q", m.Spec.ReinvocationPolicy)
	}

	if len(m.Spec.MatchConstraints.ResourceRules) != 1 {
		t.Fatalf("expected 1 resource rule, got %d", len(m.Spec.MatchConstraints.ResourceRules))
	}

	rule := m.Spec.MatchConstraints.ResourceRules[0]
	if rule.Resources[0] != "pods" {
		t.Fatalf("expected resource pods, got %v", rule.Resources)
	}

	if len(m.Spec.MatchConditions) != 2 {
		t.Fatalf("expected 2 matchConditions, got %d", len(m.Spec.MatchConditions))
	}

	if m.Spec.MatchConditions[0].Name != "is-virt-launcher-pod" {
		t.Fatalf("expected matchCondition name is-virt-launcher-pod, got %q", m.Spec.MatchConditions[0].Name)
	}

	if m.Spec.MatchConditions[1].Name != "has-plugin-socket-volume" {
		t.Fatalf("expected matchCondition name has-plugin-socket-volume, got %q", m.Spec.MatchConditions[1].Name)
	}

	if len(m.Spec.Mutations) != 1 {
		t.Fatalf("expected 1 mutation, got %d", len(m.Spec.Mutations))
	}

	if m.Spec.Mutations[0].PatchType != "JSONPatch" {
		t.Fatalf("expected patchType JSONPatch, got %q", m.Spec.Mutations[0].PatchType)
	}

	expr := m.Spec.Mutations[0].JSONPatch.Expression
	if !strings.Contains(expr, "plugin-sidecar-test-plugin") {
		t.Fatalf("expected JSONPatch expression to contain container name, got:\n%s", expr)
	}

	if !strings.Contains(expr, "kubevirt-plugin-sockets") {
		t.Fatalf("expected JSONPatch expression to contain volume name, got:\n%s", expr)
	}

	if !strings.Contains(expr, "/var/run/kubevirt-plugin/test-plugin/") {
		t.Fatalf("expected JSONPatch expression to contain mountPath, got:\n%s", expr)
	}

	if !strings.Contains(expr, `subPath: "test-plugin"`) {
		t.Fatalf("expected JSONPatch expression to contain subPath, got:\n%s", expr)
	}

	if !strings.Contains(expr, `args: ["--entrypoint", "test-plugin", "--role", "sidecar"]`) {
		t.Fatalf("expected JSONPatch expression to pass sidecar args, got:\n%s", expr)
	}

	if strings.Contains(expr, "ephemeral-disks") {
		t.Fatalf("expected no ephemeral-disks mount without a PreBoot hook, got:\n%s", expr)
	}

	mustParseCEL(t, expr)
}

func TestGenerateMAPSidecarSecurityContext(t *testing.T) {
	p := New("test-plugin").WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{}))
	_, outputDir := generatePlugin(t, p)

	var m testMAP
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "mutating-admission-policy.yaml"), &m)

	expr := m.Spec.Mutations[0].JSONPatch.Expression
	if !strings.Contains(expr, "runAsUser: 107") {
		t.Fatalf("expected JSONPatch expression to run as the qemu uid, got:\n%s", expr)
	}

	if !strings.Contains(expr, "runAsGroup: 107") {
		t.Fatalf("expected JSONPatch expression to run as the qemu gid, got:\n%s", expr)
	}

	if !strings.Contains(expr, "runAsNonRoot: true") {
		t.Fatalf("expected JSONPatch expression to keep runAsNonRoot, got:\n%s", expr)
	}

	if strings.Contains(expr, "imagePullPolicy") {
		t.Fatalf("expected no imagePullPolicy when none is set, got:\n%s", expr)
	}

	if strings.Contains(expr, "resources:") {
		t.Fatalf("expected no resources when none are set, got:\n%s", expr)
	}

	mustParseCEL(t, expr)
}

func TestGenerateMAPWithImagePullPolicy(t *testing.T) {
	p := New("test-plugin").
		WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{})).
		WithImagePullPolicy("Always")
	_, outputDir := generatePlugin(t, p)

	var m testMAP
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "mutating-admission-policy.yaml"), &m)

	expr := m.Spec.Mutations[0].JSONPatch.Expression
	if !strings.Contains(expr, `imagePullPolicy: "Always"`) {
		t.Fatalf("expected JSONPatch expression to honor the image pull policy, got:\n%s", expr)
	}

	mustParseCEL(t, expr)
}

func TestGenerateMAPMountsEphemeralDisksForPreBoot(t *testing.T) {
	p := New("test-plugin").
		WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{})).
		WithLauncherHook(PreBoot(&stubPreBootHandler{}).WithEntrypoint("ep-preboot"))
	_, outputDir := generatePlugin(t, p)

	var guestDefinitionMAP testMAP
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "mutating-admission-policy.yaml"), &guestDefinitionMAP)
	if strings.Contains(guestDefinitionMAP.Spec.Mutations[0].JSONPatch.Expression, "ephemeral-disks") {
		t.Fatalf("expected no ephemeral-disks mount for the GuestDefinition entrypoint, got:\n%s", guestDefinitionMAP.Spec.Mutations[0].JSONPatch.Expression)
	}

	var preBootMAP testMAP
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "ep-preboot-mutating-admission-policy.yaml"), &preBootMAP)
	expr := preBootMAP.Spec.Mutations[0].JSONPatch.Expression
	if !strings.Contains(expr, `name: "ephemeral-disks"`) {
		t.Fatalf("expected the ephemeral-disks volume mount for the PreBoot entrypoint, got:\n%s", expr)
	}

	if !strings.Contains(expr, `mountPath: "/var/run/kubevirt-ephemeral-disks"`) {
		t.Fatalf("expected the ephemeral-disks mount path for the PreBoot entrypoint, got:\n%s", expr)
	}

	mustParseCEL(t, expr)
}

func TestGenerateMAPWithSidecarPodLabels(t *testing.T) {
	p := New("test-plugin").
		WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{})).
		WithSidecarPodLabels(map[string]string{"zeta": "1", "alpha": "2"})
	_, outputDir := generatePlugin(t, p)

	var m testMAP
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "mutating-admission-policy.yaml"), &m)

	if len(m.Spec.MatchConditions) != 4 {
		t.Fatalf("expected 4 matchConditions, got %d: %+v", len(m.Spec.MatchConditions), m.Spec.MatchConditions)
	}

	conditions := map[string]string{}
	for _, condition := range m.Spec.MatchConditions {
		conditions[condition.Name] = condition.Expression
	}

	expectedAlpha := `has(object.metadata.labels) && "alpha" in object.metadata.labels && object.metadata.labels["alpha"] == "2"`
	if conditions["has-target-label-0"] != expectedAlpha {
		t.Fatalf("expected the first label matchCondition to gate on alpha, got %q", conditions["has-target-label-0"])
	}

	expectedZeta := `has(object.metadata.labels) && "zeta" in object.metadata.labels && object.metadata.labels["zeta"] == "1"`
	if conditions["has-target-label-1"] != expectedZeta {
		t.Fatalf("expected the second label matchCondition to gate on zeta, got %q", conditions["has-target-label-1"])
	}

	for _, condition := range m.Spec.MatchConditions[2:] {
		mustParseCEL(t, condition.Expression)
	}
}

func TestGenerateMAPWithSidecarResources(t *testing.T) {
	p := New("test-plugin").
		WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{})).
		WithSidecarResources(corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("64Mi"), corev1.ResourceCPU: resource.MustParse("50m")},
			Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m")},
		})
	_, outputDir := generatePlugin(t, p)

	var m testMAP
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "mutating-admission-policy.yaml"), &m)

	expr := m.Spec.Mutations[0].JSONPatch.Expression
	if !strings.Contains(expr, "resources: Object.spec.containers.resources{") {
		t.Fatalf("expected JSONPatch expression to render the resources, got:\n%s", expr)
	}

	if !strings.Contains(expr, `requests: {"cpu": "50m", "memory": "64Mi"}`) {
		t.Fatalf("expected JSONPatch expression to render sorted requests, got:\n%s", expr)
	}

	if !strings.Contains(expr, `limits: {"cpu": "500m"}`) {
		t.Fatalf("expected JSONPatch expression to render limits, got:\n%s", expr)
	}

	mustParseCEL(t, expr)
}

func TestGenerateMAPBinding(t *testing.T) {
	p := New("test-plugin").WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{}))
	_, outputDir := generatePlugin(t, p)

	var mb testMAPBinding
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "mutating-admission-policy-binding.yaml"), &mb)

	if mb.Kind != "MutatingAdmissionPolicyBinding" {
		t.Fatalf("expected kind MutatingAdmissionPolicyBinding, got %q", mb.Kind)
	}

	if mb.Spec.PolicyName != "test-plugin" {
		t.Fatalf("expected policyName test-plugin, got %q", mb.Spec.PolicyName)
	}
}

func TestGenerateDockerfile(t *testing.T) {
	p := New("test-plugin").WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{}))
	sourceDir, _ := generatePlugin(t, p)

	content := readFileContent(t, filepath.Join(sourceDir, "Dockerfile"))

	if !strings.Contains(content, "FROM golang:1.26 AS builder") {
		t.Fatalf("expected Dockerfile to contain Go version 1.26, got:\n%s", content)
	}

	if !strings.Contains(content, `ENTRYPOINT ["/plugin", "serve"]`) {
		t.Fatalf("expected Dockerfile to contain ENTRYPOINT, got:\n%s", content)
	}
}

func TestGenerateMakefile(t *testing.T) {
	p := New("test-plugin").WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{}))
	sourceDir, _ := generatePlugin(t, p)

	content := readFileContent(t, filepath.Join(sourceDir, "Makefile"))

	if !strings.Contains(content, "test-plugin") {
		t.Fatalf("expected Makefile to contain plugin name, got:\n%s", content)
	}

	if !strings.Contains(content, "podman") {
		t.Fatalf("expected Makefile to contain podman detection, got:\n%s", content)
	}

	if !strings.Contains(content, "docker") {
		t.Fatalf("expected Makefile to contain docker fallback, got:\n%s", content)
	}

	if !strings.Contains(content, "GO_VERSION ?= 1.26") {
		t.Fatalf("expected Makefile to contain GO_VERSION 1.26, got:\n%s", content)
	}

	if !strings.Contains(content, "go test ./...") {
		t.Fatalf("expected Makefile to contain test target, got:\n%s", content)
	}

	if !strings.Contains(content, "go vet ./...") {
		t.Fatalf("expected Makefile to contain vet target, got:\n%s", content)
	}
}

func TestGenerateMakefilePushesManifestImage(t *testing.T) {
	tests := []struct {
		name  string
		image string
	}{
		{name: "default image"},
		{name: "custom image", image: "example.com/myorg/test-plugin:v1.2.3"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := New("test-plugin").
				WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{})).
				WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{}))
			if test.image != "" {
				p = p.WithImage(test.image)
			}
			sourceDir, outputDir := generatePlugin(t, p)

			var ds testDaemonSet
			readAndUnmarshal(t, findGeneratedFile(t, outputDir, "daemonset.yaml"), &ds)
			image := ds.Spec.Template.Spec.Containers[0].Image

			mapContent := readFileContent(t, findGeneratedFile(t, outputDir, "mutating-admission-policy.yaml"))
			if !strings.Contains(mapContent, "image: \""+image+"\"") {
				t.Fatalf("expected MAP to reference image %q, got:\n%s", image, mapContent)
			}

			makefile := readFileContent(t, filepath.Join(sourceDir, "Makefile"))
			expected := "IMAGE = " + image
			if !strings.Contains(makefile, expected) {
				t.Fatalf("expected Makefile to push the manifest image via %q, got:\n%s", expected, makefile)
			}
		})
	}
}

func TestBuilderGoVersion(t *testing.T) {
	tests := []struct {
		goDirective string
		expected    string
	}{
		{goDirective: "1.24.0", expected: "1.26"},
		{goDirective: "1.26", expected: "1.26"},
		{goDirective: "1.26.3", expected: "1.26"},
		{goDirective: "1.27.1", expected: "1.27"},
		{goDirective: "not-a-version", expected: "1.26"},
	}

	for _, test := range tests {
		t.Run(test.goDirective, func(t *testing.T) {
			if got := builderGoVersion(test.goDirective); got != test.expected {
				t.Fatalf("builderGoVersion(%q) = %q, expected %q", test.goDirective, got, test.expected)
			}
		})
	}
}

func TestGenerateCreatesOutputDir(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "nested", "deploy")

	p := New("test-plugin").WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{}))
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(outputDir); os.IsNotExist(err) {
		t.Fatal("expected output directory to be created")
	}

	entries, err := os.ReadDir(outputDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) == 0 {
		t.Fatal("expected files in output directory")
	}
}

func TestGenerateFileNumbering(t *testing.T) {
	rules := []rbacv1.PolicyRule{{
		APIGroups: []string{""},
		Resources: []string{"pods"},
		Verbs:     []string{"get"},
	}}

	p := New("test-plugin").
		WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{})).
		WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{})).
		WithRBACRules(rules)
	sourceDir, outputDir := generatePlugin(t, p)

	expectedYAML := []string{
		"01-rbac.yaml",
		"02-daemonset.yaml",
		"03-plugin.yaml",
		"04-mutating-admission-policy.yaml",
		"05-mutating-admission-policy-binding.yaml",
	}

	files := listFileNames(outputDir)
	if len(files) != len(expectedYAML) {
		t.Fatalf("expected %d YAML files in output dir, got %d: %v", len(expectedYAML), len(files), files)
	}

	for i, name := range expectedYAML {
		if files[i] != name {
			t.Fatalf("file %d: expected %q, got %q (all files: %v)", i, name, files[i], files)
		}
	}

	for _, name := range []string{"Dockerfile", "Makefile"} {
		if _, err := os.Stat(filepath.Join(sourceDir, name)); os.IsNotExist(err) {
			t.Fatalf("expected %s in source dir %s", name, sourceDir)
		}
	}
}

// --- Guard clause tests ---

func TestGenerateWithNoHooks(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	err := New("empty-plugin").generate(outputDir, sourceDir)
	if err == nil {
		t.Fatal("expected error when generating with no hooks")
	}

	if !strings.Contains(err.Error(), "no hooks registered") {
		t.Fatalf("expected 'no hooks registered' error, got: %v", err)
	}
}

func TestReadGoVersionMissingGoMod(t *testing.T) {
	dir := t.TempDir()

	_, err := readGoVersion(dir)
	if err == nil {
		t.Fatal("expected error when go.mod is missing")
	}

	if !strings.Contains(err.Error(), "go.mod") {
		t.Fatalf("expected go.mod-related error, got: %v", err)
	}
}

func TestReadGoVersionMissingGoLine(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\nrequire (\n)\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := readGoVersion(dir)
	if err == nil {
		t.Fatal("expected error when go.mod has no 'go' directive")
	}

	if !strings.Contains(err.Error(), "go version not found") {
		t.Fatalf("expected 'go version not found' error, got: %v", err)
	}
}

func TestGenerateMultiEntrypointDaemonSets(t *testing.T) {
	p := New("test-plugin").
		WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{}).WithEntrypoint("ep-a")).
		WithNodeHook(NodeHookPostVMStop, NodeHandler(&stubNodeHandler{}).WithEntrypoint("ep-b"))
	_, outputDir := generatePlugin(t, p)

	var dsA testDaemonSet
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "ep-a-daemonset.yaml"), &dsA)

	if dsA.Metadata.Name != "test-plugin-ep-a" {
		t.Fatalf("expected DaemonSet name 'test-plugin-ep-a', got %q", dsA.Metadata.Name)
	}

	var dsB testDaemonSet
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "ep-b-daemonset.yaml"), &dsB)

	if dsB.Metadata.Name != "test-plugin-ep-b" {
		t.Fatalf("expected DaemonSet name 'test-plugin-ep-b', got %q", dsB.Metadata.Name)
	}
}

func TestGenerateMultiEntrypointMAPs(t *testing.T) {
	p := New("test-plugin").
		WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{}).WithEntrypoint("ep-a")).
		WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{}).WithEntrypoint("ep-b"))
	_, outputDir := generatePlugin(t, p)

	var mapA testMAP
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "ep-a-mutating-admission-policy.yaml"), &mapA)

	if mapA.Metadata.Name != "test-plugin-ep-a" {
		t.Fatalf("expected MAP name 'test-plugin-ep-a', got %q", mapA.Metadata.Name)
	}

	var mapB testMAP
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "ep-b-mutating-admission-policy.yaml"), &mapB)

	if mapB.Metadata.Name != "test-plugin-ep-b" {
		t.Fatalf("expected MAP name 'test-plugin-ep-b', got %q", mapB.Metadata.Name)
	}

	var bindingA testMAPBinding
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "ep-a-mutating-admission-policy-binding.yaml"), &bindingA)

	if bindingA.Spec.PolicyName != "test-plugin-ep-a" {
		t.Fatalf("expected binding policyName 'test-plugin-ep-a', got %q", bindingA.Spec.PolicyName)
	}

	var bindingB testMAPBinding
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "ep-b-mutating-admission-policy-binding.yaml"), &bindingB)

	if bindingB.Spec.PolicyName != "test-plugin-ep-b" {
		t.Fatalf("expected binding policyName 'test-plugin-ep-b', got %q", bindingB.Spec.PolicyName)
	}
}

func TestGenerateMultiEntrypointPluginCR(t *testing.T) {
	p := New("test-plugin").
		WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{}).WithEntrypoint("ep-a")).
		WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{}).WithEntrypoint("ep-b")).
		WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{}).WithEntrypoint("ep-a"))
	_, outputDir := generatePlugin(t, p)

	cr := readPluginCR(t, outputDir)

	if cr.Name != "test-plugin" {
		t.Fatalf("expected single Plugin CR named 'test-plugin', got %q", cr.Name)
	}

	if len(cr.Spec.LauncherHooks) != 2 {
		t.Fatalf("expected 2 launcher hooks in CR, got %d", len(cr.Spec.LauncherHooks))
	}

	if len(cr.Spec.NodeHooks) != 1 {
		t.Fatalf("expected 1 nodeHook in CR, got %d", len(cr.Spec.NodeHooks))
	}

	if cr.Spec.LauncherHooks[0].Sidecar.SocketPath != "/var/run/kubevirt-plugin/test-plugin/ep-a/launcher.sock" {
		t.Fatalf("expected ep-a socket path, got %q", cr.Spec.LauncherHooks[0].Sidecar.SocketPath)
	}

	if cr.Spec.LauncherHooks[1].Sidecar.SocketPath != "/var/run/kubevirt-plugin/test-plugin/ep-b/launcher.sock" {
		t.Fatalf("expected ep-b socket path, got %q", cr.Spec.LauncherHooks[1].Sidecar.SocketPath)
	}
}

func TestGenerateDaemonSetContainerArgs(t *testing.T) {
	p := New("test-plugin").
		WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{}).WithEntrypoint("ep-a"))
	_, outputDir := generatePlugin(t, p)

	var ds testDaemonSet
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "ep-a-daemonset.yaml"), &ds)

	container := ds.Spec.Template.Spec.Containers[0]

	expectedArgs := []string{"--entrypoint", "ep-a", "--role", "daemonset"}
	if !slices.Equal(container.Args, expectedArgs) {
		t.Fatalf("expected args %v, got %v", expectedArgs, container.Args)
	}

	if !strings.Contains(container.Image, "test-plugin") {
		t.Fatalf("expected image to use plugin name, got %q", container.Image)
	}
}

func TestGenerateMAPContainerArgs(t *testing.T) {
	p := New("test-plugin").
		WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{}).WithEntrypoint("ep-a"))
	_, outputDir := generatePlugin(t, p)

	var policy testMAP
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "ep-a-mutating-admission-policy.yaml"), &policy)

	expr := policy.Spec.Mutations[0].JSONPatch.Expression
	if !strings.Contains(expr, `args: ["--entrypoint", "ep-a", "--role", "sidecar"]`) {
		t.Fatalf("expected JSONPatch expression to pass sidecar args, got:\n%s", expr)
	}

	if strings.Contains(expr, "command:") {
		t.Fatalf("expected JSONPatch expression to keep the image entrypoint, got:\n%s", expr)
	}

	if !strings.Contains(expr, `subPath: "test-plugin"`) {
		t.Fatalf("expected JSONPatch expression to mount the sockets volume with a subPath, got:\n%s", expr)
	}

	mustParseCEL(t, expr)
}

func TestGenerateSingleEntrypointBackwardCompat(t *testing.T) {
	p := New("test-plugin").
		WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{})).
		WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{}))
	_, outputDir := generatePlugin(t, p)

	findGeneratedFile(t, outputDir, "daemonset.yaml")
	findGeneratedFile(t, outputDir, "mutating-admission-policy.yaml")
	findGeneratedFile(t, outputDir, "mutating-admission-policy-binding.yaml")
	findGeneratedFile(t, outputDir, "plugin.yaml")

	var ds testDaemonSet
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "daemonset.yaml"), &ds)

	if ds.Metadata.Name != "test-plugin" {
		t.Fatalf("expected DaemonSet name 'test-plugin', got %q", ds.Metadata.Name)
	}

	container := ds.Spec.Template.Spec.Containers[0]
	expectedArgs := []string{"--entrypoint", "test-plugin", "--role", "daemonset"}
	if !slices.Equal(container.Args, expectedArgs) {
		t.Fatalf("expected args %v, got %v", expectedArgs, container.Args)
	}

	var m testMAP
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "mutating-admission-policy.yaml"), &m)

	if m.Metadata.Name != "test-plugin" {
		t.Fatalf("expected MAP name 'test-plugin', got %q", m.Metadata.Name)
	}
}

func TestGenerateNodeSocketPathSubdirectory(t *testing.T) {
	p := New("test-plugin").
		WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{}).WithEntrypoint("ep-a"))
	_, outputDir := generatePlugin(t, p)

	cr := readPluginCR(t, outputDir)

	expected := "/var/run/kubevirt/plugins/test-plugin/ep-a/node.sock"
	if cr.Spec.NodeHooks[0].Socket != expected {
		t.Fatalf("expected socket path %q, got %q", expected, cr.Spec.NodeHooks[0].Socket)
	}
}

func TestGenerateCELWithNodeHook(t *testing.T) {
	p := New("test-plugin").
		WithLauncherHook(GuestDefinitionCEL("Domain{Name: 'test'}")).
		WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{}))
	sourceDir, outputDir := generatePlugin(t, p)

	cr := readPluginCR(t, outputDir)

	if len(cr.Spec.LauncherHooks) != 1 {
		t.Fatalf("expected 1 launcher hook, got %d", len(cr.Spec.LauncherHooks))
	}

	if cr.Spec.LauncherHooks[0].CEL == nil {
		t.Fatal("expected CEL launcher hook")
	}

	if len(cr.Spec.NodeHooks) != 1 {
		t.Fatalf("expected 1 nodeHook, got %d", len(cr.Spec.NodeHooks))
	}

	// DaemonSet should be generated (for node hook)
	findGeneratedFile(t, outputDir, "daemonset.yaml")

	// MAP should NOT be generated (no sidecar launcher hooks)
	if generatedFileExists(outputDir, "mutating-admission-policy.yaml") {
		t.Fatal("expected no MAP for CEL-only launcher hooks")
	}

	// Dockerfile should be generated (node hook needs container)
	if _, err := os.Stat(filepath.Join(sourceDir, "Dockerfile")); os.IsNotExist(err) {
		t.Fatal("expected Dockerfile for node hook plugin")
	}
}

func TestGenerateCELOnlyGeneratesNoBuildOrAdmissionFiles(t *testing.T) {
	p := New("test-plugin").WithLauncherHook(GuestDefinitionCEL("Domain{Name: 'test'}"))
	sourceDir, outputDir := generatePlugin(t, p)

	for _, name := range []string{"Dockerfile", "Makefile"} {
		if _, err := os.Stat(filepath.Join(sourceDir, name)); !os.IsNotExist(err) {
			t.Fatalf("CEL-only plugin should not generate %s", name)
		}
	}

	for _, suffix := range []string{"mutating-admission-policy.yaml", "mutating-admission-policy-binding.yaml"} {
		if generatedFileExists(outputDir, suffix) {
			t.Fatalf("CEL-only plugin should not generate %s", suffix)
		}
	}
}

func TestGenerateMixedSidecarAndCEL(t *testing.T) {
	p := New("test-plugin").
		WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{})).
		WithLauncherHook(GuestDefinitionCEL("Domain{Name: 'mutated'}"))
	_, outputDir := generatePlugin(t, p)

	cr := readPluginCR(t, outputDir)

	if len(cr.Spec.LauncherHooks) != 2 {
		t.Fatalf("expected 2 launcher hooks, got %d", len(cr.Spec.LauncherHooks))
	}

	if cr.Spec.LauncherHooks[0].Sidecar == nil {
		t.Fatal("expected first hook to be sidecar")
	}

	if cr.Spec.LauncherHooks[1].CEL == nil {
		t.Fatal("expected second hook to be CEL")
	}

	if cr.Spec.LauncherHooks[1].CEL.Expression != "Domain{Name: 'mutated'}" {
		t.Fatalf("expected CEL expression, got %q", cr.Spec.LauncherHooks[1].CEL.Expression)
	}

	// MAP should still be generated for the sidecar hook
	findGeneratedFile(t, outputDir, "mutating-admission-policy.yaml")
}

func TestGenerateCELWithPerHookSettings(t *testing.T) {
	p := New("test-plugin").WithLauncherHook(
		GuestDefinitionCEL("Domain{Name: 'test'}").
			WithCondition("vmi.Name == 'my-vmi'").
			WithFailureStrategy(Ignore),
	)
	_, outputDir := generatePlugin(t, p)

	cr := readPluginCR(t, outputDir)

	if len(cr.Spec.LauncherHooks) != 1 {
		t.Fatalf("expected 1 launcher hook, got %d", len(cr.Spec.LauncherHooks))
	}

	hook := cr.Spec.LauncherHooks[0]

	if hook.Condition != "vmi.Name == 'my-vmi'" {
		t.Fatalf("expected condition, got %q", hook.Condition)
	}

	if hook.FailureStrategy != pluginv1alpha1.FailureStrategyIgnore {
		t.Fatalf("expected failureStrategy Ignore, got %q", hook.FailureStrategy)
	}

	if hook.Timeout != nil {
		t.Fatalf("expected no timeout on a CEL hook, got %v", hook.Timeout)
	}
}

func TestGenerateCELExpressionWithDoubleQuotes(t *testing.T) {
	expr := `Domain{Title: "test"}`
	p := New("test-plugin").WithLauncherHook(GuestDefinitionCEL(expr))
	_, outputDir := generatePlugin(t, p)

	cr := readPluginCR(t, outputDir)

	if len(cr.Spec.LauncherHooks) != 1 || cr.Spec.LauncherHooks[0].CEL == nil {
		t.Fatal("expected 1 CEL launcher hook")
	}

	if cr.Spec.LauncherHooks[0].CEL.Expression != expr {
		t.Fatalf("expected expression to round-trip, got %q", cr.Spec.LauncherHooks[0].CEL.Expression)
	}
}
