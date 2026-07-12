package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v2"
	rbacv1 "k8s.io/api/rbac/v1"
)

// Test-local types for YAML verification

type testPluginCR struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Condition       string `yaml:"condition,omitempty"`
		FailureStrategy string `yaml:"failureStrategy,omitempty"`
		DomainHooks     []struct {
			Sidecar struct {
				SocketPath string `yaml:"socketPath"`
			} `yaml:"sidecar"`
			Condition       string `yaml:"condition,omitempty"`
			FailureStrategy string `yaml:"failureStrategy,omitempty"`
			Timeout         string `yaml:"timeout,omitempty"`
		} `yaml:"domainHooks,omitempty"`
		NodeHooks []struct {
			HookPoint       string `yaml:"hookPoint"`
			Socket          string `yaml:"socket"`
			Condition       string `yaml:"condition,omitempty"`
			FailureStrategy string `yaml:"failureStrategy,omitempty"`
			Timeout         string `yaml:"timeout,omitempty"`
		} `yaml:"nodeHooks,omitempty"`
	} `yaml:"spec"`
}

type testDaemonSet struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name   string            `yaml:"name"`
		Labels map[string]string `yaml:"labels"`
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
		Name string `yaml:"name"`
	} `yaml:"metadata"`
}

type testClusterRole struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Rules []struct {
		APIGroups []string `yaml:"apiGroups"`
		Resources []string `yaml:"resources"`
		Verbs     []string `yaml:"verbs"`
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
		MatchConstraints struct {
			ResourceRules []struct {
				APIGroups   []string `yaml:"apiGroups"`
				APIVersions []string `yaml:"apiVersions"`
				Resources   []string `yaml:"resources"`
				Operations  []string `yaml:"operations"`
			} `yaml:"resourceRules"`
		} `yaml:"matchConstraints"`
		ParamKind struct {
			APIVersion string `yaml:"apiVersion"`
			Kind       string `yaml:"kind"`
		} `yaml:"paramKind"`
		ReinvocationPolicy string `yaml:"reinvocationPolicy"`
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
		ParamRef   struct {
			Name string `yaml:"name"`
		} `yaml:"paramRef"`
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

func readAndUnmarshal(t *testing.T, path string, v any) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}

	if err := yaml.Unmarshal(data, v); err != nil {
		t.Fatalf("failed to unmarshal %s: %v\nContent:\n%s", path, err, string(data))
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

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read dir %s: %v", dir, err)
	}

	for _, e := range entries {
		if strings.HasSuffix(e.Name(), suffix) {
			return filepath.Join(dir, e.Name())
		}
	}

	t.Fatalf("no file with suffix %q found in %s (files: %v)", suffix, dir, listFileNames(dir))
	return ""
}

func generatedFileExists(dir, suffix string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}

	for _, e := range entries {
		if strings.HasSuffix(e.Name(), suffix) {
			return true
		}
	}

	return false
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

// Tests

func TestGeneratePluginCRNodeHookOnly(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	p := New("test-plugin").WithNodeHook(PreVMStart, NodeHandler(&stubNodeHandler{}))
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

	var cr testPluginCR
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "plugin.yaml"), &cr)

	if cr.APIVersion != "plugin.kubevirt.io/v1alpha1" {
		t.Fatalf("expected apiVersion plugin.kubevirt.io/v1alpha1, got %q", cr.APIVersion)
	}

	if cr.Kind != "Plugin" {
		t.Fatalf("expected kind Plugin, got %q", cr.Kind)
	}

	if cr.Metadata.Name != "test-plugin" {
		t.Fatalf("expected name test-plugin, got %q", cr.Metadata.Name)
	}

	if len(cr.Spec.NodeHooks) != 1 {
		t.Fatalf("expected 1 nodeHook, got %d", len(cr.Spec.NodeHooks))
	}

	if cr.Spec.NodeHooks[0].HookPoint != PreVMStart {
		t.Fatalf("expected hookPoint %q, got %q", PreVMStart, cr.Spec.NodeHooks[0].HookPoint)
	}

	if cr.Spec.NodeHooks[0].Socket != "/var/run/kubevirt/plugins/test-plugin/node.sock" {
		t.Fatalf("expected socket /var/run/kubevirt/plugins/test-plugin/node.sock, got %q", cr.Spec.NodeHooks[0].Socket)
	}

	if len(cr.Spec.DomainHooks) != 0 {
		t.Fatal("expected no domainHooks")
	}
}

func TestGeneratePluginCRDomainHookOnly(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	p := New("test-plugin").WithDomainHook(ForLibvirt(&stubDomainHandler{}))
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

	var cr testPluginCR
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "plugin.yaml"), &cr)

	if len(cr.Spec.DomainHooks) != 1 {
		t.Fatalf("expected 1 domainHook, got %d", len(cr.Spec.DomainHooks))
	}

	if cr.Spec.DomainHooks[0].Sidecar.SocketPath != "/var/run/kubevirt-plugin/test-plugin/domain.sock" {
		t.Fatalf("expected sidecar socket path, got %q", cr.Spec.DomainHooks[0].Sidecar.SocketPath)
	}

	if len(cr.Spec.NodeHooks) != 0 {
		t.Fatal("expected no nodeHooks")
	}
}

func TestGeneratePluginCRBothHooks(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	p := New("test-plugin").
		WithDomainHook(ForLibvirt(&stubDomainHandler{})).
		WithNodeHook(PreVMStart, NodeHandler(&stubNodeHandler{}))
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

	var cr testPluginCR
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "plugin.yaml"), &cr)

	if len(cr.Spec.DomainHooks) != 1 {
		t.Fatalf("expected 1 domainHook, got %d", len(cr.Spec.DomainHooks))
	}

	if len(cr.Spec.NodeHooks) != 1 {
		t.Fatalf("expected 1 nodeHook, got %d", len(cr.Spec.NodeHooks))
	}
}

func TestGeneratePluginCRWithCondition(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	p := New("test-plugin").
		WithNodeHook(PreVMStart, NodeHandler(&stubNodeHandler{})).
		WithCondition("vmi.labels.gpu == 'true'")
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

	var cr testPluginCR
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "plugin.yaml"), &cr)

	if cr.Spec.Condition != "vmi.labels.gpu == 'true'" {
		t.Fatalf("expected condition, got %q", cr.Spec.Condition)
	}
}

func TestGeneratePluginCRWithPerHookSettings(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	timeout := 30 * time.Second
	p := New("test-plugin").WithNodeHook(PreVMStart,
		NodeHandler(&stubNodeHandler{}).
			WithCondition("vmi.metadata.name == 'test'").
			WithFailureStrategy(Ignore).
			WithTimeout(timeout),
	)
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

	var cr testPluginCR
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "plugin.yaml"), &cr)

	if len(cr.Spec.NodeHooks) != 1 {
		t.Fatalf("expected 1 nodeHook, got %d", len(cr.Spec.NodeHooks))
	}

	nodeHook := cr.Spec.NodeHooks[0]

	if nodeHook.Condition != "vmi.metadata.name == 'test'" {
		t.Fatalf("expected per-hook condition, got %q", nodeHook.Condition)
	}

	if nodeHook.FailureStrategy != "Ignore" {
		t.Fatalf("expected failureStrategy Ignore, got %q", nodeHook.FailureStrategy)
	}

	if nodeHook.Timeout != "30s" {
		t.Fatalf("expected timeout 30s, got %q", nodeHook.Timeout)
	}
}

func TestGenerateDaemonSet(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	p := New("test-plugin").WithNodeHook(PreVMStart, NodeHandler(&stubNodeHandler{}))
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

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
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	rules := []rbacv1.PolicyRule{{
		APIGroups: []string{""},
		Resources: []string{"pods"},
		Verbs:     []string{"get", "list"},
	}}

	p := New("test-plugin").
		WithNodeHook(PreVMStart, NodeHandler(&stubNodeHandler{})).
		WithRBACRules(rules)
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

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
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	rules := []rbacv1.PolicyRule{{
		APIGroups: []string{""},
		Resources: []string{"pods"},
		Verbs:     []string{"get", "list"},
	}}

	p := New("test-plugin").
		WithNodeHook(PreVMStart, NodeHandler(&stubNodeHandler{})).
		WithRBACRules(rules).
		WithNamespace("kube-system")
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

	rbacFile := findGeneratedFile(t, outputDir, "rbac.yaml")
	content := readFileContent(t, rbacFile)
	docs := splitYAMLDocuments(content)
	if len(docs) != 3 {
		t.Fatalf("expected 3 YAML documents, got %d", len(docs))
	}

	var crb testClusterRoleBinding
	if err := yaml.Unmarshal([]byte(docs[2]), &crb); err != nil {
		t.Fatalf("unmarshal ClusterRoleBinding: %v", err)
	}

	if crb.Subjects[0].Namespace != "kube-system" {
		t.Fatalf("expected namespace kube-system, got %q", crb.Subjects[0].Namespace)
	}
}

func TestGenerateRBACWithResourceNames(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	rules := []rbacv1.PolicyRule{{
		APIGroups:     []string{""},
		Resources:     []string{"configmaps"},
		ResourceNames: []string{"my-config"},
		Verbs:         []string{"get"},
	}}

	p := New("test-plugin").
		WithNodeHook(PreVMStart, NodeHandler(&stubNodeHandler{})).
		WithRBACRules(rules)
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

	rbacFile := findGeneratedFile(t, outputDir, "rbac.yaml")
	content := readFileContent(t, rbacFile)

	if !strings.Contains(content, "resourceNames") {
		t.Fatalf("expected rbac.yaml to contain resourceNames, got:\n%s", content)
	}

	if !strings.Contains(content, "my-config") {
		t.Fatalf("expected rbac.yaml to contain my-config, got:\n%s", content)
	}
}

func TestGenerateNoRBACWhenNoRules(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	p := New("test-plugin").WithNodeHook(PreVMStart, NodeHandler(&stubNodeHandler{}))
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

	if generatedFileExists(outputDir, "rbac.yaml") {
		t.Fatal("expected no rbac.yaml when no RBAC rules provided")
	}
}

func TestGenerateMAP(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	p := New("test-plugin").WithDomainHook(ForLibvirt(&stubDomainHandler{}))
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

	var m testMAP
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "mutating-admission-policy.yaml"), &m)

	if m.APIVersion != "admissionregistration.k8s.io/v1alpha1" {
		t.Fatalf("expected apiVersion admissionregistration.k8s.io/v1alpha1, got %q", m.APIVersion)
	}

	if m.Kind != "MutatingAdmissionPolicy" {
		t.Fatalf("expected kind MutatingAdmissionPolicy, got %q", m.Kind)
	}

	if m.Metadata.Name != "test-plugin" {
		t.Fatalf("expected name test-plugin, got %q", m.Metadata.Name)
	}

	if m.Spec.ReinvocationPolicy != "IfNeeded" {
		t.Fatalf("expected reinvocationPolicy IfNeeded, got %q", m.Spec.ReinvocationPolicy)
	}

	if m.Spec.ParamKind.APIVersion != "plugin.kubevirt.io/v1alpha1" {
		t.Fatalf("expected paramKind apiVersion, got %q", m.Spec.ParamKind.APIVersion)
	}

	if m.Spec.ParamKind.Kind != "Plugin" {
		t.Fatalf("expected paramKind kind Plugin, got %q", m.Spec.ParamKind.Kind)
	}
}

func TestGenerateMAPBinding(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	p := New("test-plugin").WithDomainHook(ForLibvirt(&stubDomainHandler{}))
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

	var mb testMAPBinding
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "mutating-admission-policy-binding.yaml"), &mb)

	if mb.Kind != "MutatingAdmissionPolicyBinding" {
		t.Fatalf("expected kind MutatingAdmissionPolicyBinding, got %q", mb.Kind)
	}

	if mb.Spec.PolicyName != "test-plugin" {
		t.Fatalf("expected policyName test-plugin, got %q", mb.Spec.PolicyName)
	}

	if mb.Spec.ParamRef.Name != "test-plugin" {
		t.Fatalf("expected paramRef name test-plugin, got %q", mb.Spec.ParamRef.Name)
	}
}

func TestGenerateDockerfile(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	p := New("test-plugin").WithNodeHook(PreVMStart, NodeHandler(&stubNodeHandler{}))
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

	content := readFileContent(t, filepath.Join(sourceDir, "Dockerfile"))

	if !strings.Contains(content, "FROM golang:1.23.0 AS builder") {
		t.Fatalf("expected Dockerfile to contain Go version 1.23.0, got:\n%s", content)
	}

	if !strings.Contains(content, `ENTRYPOINT ["/plugin", "serve"]`) {
		t.Fatalf("expected Dockerfile to contain ENTRYPOINT, got:\n%s", content)
	}
}

func TestGenerateMakefile(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	p := New("test-plugin").WithNodeHook(PreVMStart, NodeHandler(&stubNodeHandler{}))
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

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
}

func TestGenerateCreatesOutputDir(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "nested", "deploy")

	p := New("test-plugin").WithNodeHook(PreVMStart, NodeHandler(&stubNodeHandler{}))
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
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	rules := []rbacv1.PolicyRule{{
		APIGroups: []string{""},
		Resources: []string{"pods"},
		Verbs:     []string{"get"},
	}}

	p := New("test-plugin").
		WithDomainHook(ForLibvirt(&stubDomainHandler{})).
		WithNodeHook(PreVMStart, NodeHandler(&stubNodeHandler{})).
		WithRBACRules(rules)
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

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

	p := New("empty-plugin")

	err := p.generate(outputDir, sourceDir)
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

func TestGenerateRBACWithNonResourceURLs(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	rules := []rbacv1.PolicyRule{{
		NonResourceURLs: []string{"/healthz", "/metrics"},
		Verbs:           []string{"get"},
	}}

	p := New("test-plugin").
		WithNodeHook(PreVMStart, NodeHandler(&stubNodeHandler{})).
		WithRBACRules(rules)
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

	rbacFile := findGeneratedFile(t, outputDir, "rbac.yaml")
	content := readFileContent(t, rbacFile)

	if !strings.Contains(content, "nonResourceURLs") {
		t.Fatalf("expected rbac.yaml to contain nonResourceURLs, got:\n%s", content)
	}

	if !strings.Contains(content, "/healthz") {
		t.Fatalf("expected rbac.yaml to contain /healthz, got:\n%s", content)
	}

	if !strings.Contains(content, "/metrics") {
		t.Fatalf("expected rbac.yaml to contain /metrics, got:\n%s", content)
	}
}

func TestGenerateMultiEntrypointDaemonSets(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	p := New("test-plugin").
		WithNodeHook(PreVMStart, NodeHandler(&stubNodeHandler{}).WithEntrypoint("ep-a")).
		WithNodeHook(PostVMStop, NodeHandler(&stubNodeHandler{}).WithEntrypoint("ep-b"))
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

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
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	p := New("test-plugin").
		WithDomainHook(ForLibvirt(&stubDomainHandler{}).WithEntrypoint("ep-a")).
		WithDomainHook(ForLibvirt(&stubDomainHandler{}).WithEntrypoint("ep-b"))
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

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

	if bindingA.Spec.ParamRef.Name != "test-plugin" {
		t.Fatalf("expected paramRef name 'test-plugin', got %q", bindingA.Spec.ParamRef.Name)
	}

	var bindingB testMAPBinding
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "ep-b-mutating-admission-policy-binding.yaml"), &bindingB)

	if bindingB.Spec.PolicyName != "test-plugin-ep-b" {
		t.Fatalf("expected binding policyName 'test-plugin-ep-b', got %q", bindingB.Spec.PolicyName)
	}

	if bindingB.Spec.ParamRef.Name != "test-plugin" {
		t.Fatalf("expected paramRef name 'test-plugin', got %q", bindingB.Spec.ParamRef.Name)
	}
}

func TestGenerateMultiEntrypointPluginCR(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	p := New("test-plugin").
		WithDomainHook(ForLibvirt(&stubDomainHandler{}).WithEntrypoint("ep-a")).
		WithDomainHook(ForLibvirt(&stubDomainHandler{}).WithEntrypoint("ep-b")).
		WithNodeHook(PreVMStart, NodeHandler(&stubNodeHandler{}).WithEntrypoint("ep-a"))
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

	var cr testPluginCR
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "plugin.yaml"), &cr)

	if cr.Metadata.Name != "test-plugin" {
		t.Fatalf("expected single Plugin CR named 'test-plugin', got %q", cr.Metadata.Name)
	}

	if len(cr.Spec.DomainHooks) != 2 {
		t.Fatalf("expected 2 domainHooks in CR, got %d", len(cr.Spec.DomainHooks))
	}

	if len(cr.Spec.NodeHooks) != 1 {
		t.Fatalf("expected 1 nodeHook in CR, got %d", len(cr.Spec.NodeHooks))
	}

	if cr.Spec.DomainHooks[0].Sidecar.SocketPath != "/var/run/kubevirt-plugin/test-plugin/ep-a/domain.sock" {
		t.Fatalf("expected ep-a socket path, got %q", cr.Spec.DomainHooks[0].Sidecar.SocketPath)
	}

	if cr.Spec.DomainHooks[1].Sidecar.SocketPath != "/var/run/kubevirt-plugin/test-plugin/ep-b/domain.sock" {
		t.Fatalf("expected ep-b socket path, got %q", cr.Spec.DomainHooks[1].Sidecar.SocketPath)
	}
}

func TestGenerateDaemonSetContainerArgs(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	p := New("test-plugin").
		WithNodeHook(PreVMStart, NodeHandler(&stubNodeHandler{}).WithEntrypoint("ep-a"))
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

	var ds testDaemonSet
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "ep-a-daemonset.yaml"), &ds)

	container := ds.Spec.Template.Spec.Containers[0]

	if len(container.Args) != 2 || container.Args[0] != "--entrypoint" || container.Args[1] != "ep-a" {
		t.Fatalf("expected args [--entrypoint ep-a], got %v", container.Args)
	}

	if !strings.Contains(container.Image, "test-plugin") {
		t.Fatalf("expected image to use plugin name, got %q", container.Image)
	}
}

func TestGenerateSingleEntrypointBackwardCompat(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	p := New("test-plugin").
		WithDomainHook(ForLibvirt(&stubDomainHandler{})).
		WithNodeHook(PreVMStart, NodeHandler(&stubNodeHandler{}))
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

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
	if len(container.Args) != 0 {
		t.Fatalf("expected no args for default entrypoint, got %v", container.Args)
	}

	var m testMAP
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "mutating-admission-policy.yaml"), &m)

	if m.Metadata.Name != "test-plugin" {
		t.Fatalf("expected MAP name 'test-plugin', got %q", m.Metadata.Name)
	}
}

func TestGenerateDefaultEntrypointNoArgsSuffix(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	p := New("test-plugin").WithNodeHook(PreVMStart, NodeHandler(&stubNodeHandler{}))
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

	var ds testDaemonSet
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "daemonset.yaml"), &ds)

	container := ds.Spec.Template.Spec.Containers[0]
	if len(container.Args) != 0 {
		t.Fatalf("expected no args for default entrypoint, got %v", container.Args)
	}
}

func TestGenerateConflictingDomainHooksSameEntrypoint(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	t.Run("conflicting conditions", func(t *testing.T) {
		p := New("test-plugin").
			WithDomainHook(ForLibvirt(&stubDomainHandler{}).WithCondition("vmi.metadata.name == 'a'")).
			WithDomainHook(ForLibvirt(&stubDomainHandler{}).WithCondition("vmi.metadata.name == 'b'"))
		err := p.generate(outputDir, sourceDir)
		if err == nil {
			t.Fatal("expected error for conflicting conditions")
		}
		if !strings.Contains(err.Error(), "conflicting conditions") {
			t.Fatalf("expected conflicting conditions error, got: %v", err)
		}
	})

	t.Run("conflicting timeouts", func(t *testing.T) {
		p := New("test-plugin").
			WithDomainHook(ForLibvirt(&stubDomainHandler{}).WithTimeout(10 * time.Second)).
			WithDomainHook(ForLibvirt(&stubDomainHandler{}).WithTimeout(30 * time.Second))
		err := p.generate(outputDir, sourceDir)
		if err == nil {
			t.Fatal("expected error for conflicting timeouts")
		}
		if !strings.Contains(err.Error(), "conflicting timeouts") {
			t.Fatalf("expected conflicting timeouts error, got: %v", err)
		}
	})

	t.Run("conflicting failure strategies", func(t *testing.T) {
		p := New("test-plugin").
			WithDomainHook(ForLibvirt(&stubDomainHandler{}).WithFailureStrategy(Fail)).
			WithDomainHook(ForLibvirt(&stubDomainHandler{}).WithFailureStrategy(Ignore))
		err := p.generate(outputDir, sourceDir)
		if err == nil {
			t.Fatal("expected error for conflicting failure strategies")
		}
		if !strings.Contains(err.Error(), "conflicting failure strategies") {
			t.Fatalf("expected conflicting failure strategies error, got: %v", err)
		}
	})

	t.Run("one set one unset timeout", func(t *testing.T) {
		p := New("test-plugin").
			WithDomainHook(ForLibvirt(&stubDomainHandler{}).WithTimeout(10 * time.Second)).
			WithDomainHook(ForLibvirt(&stubDomainHandler{}))
		err := p.generate(outputDir, sourceDir)
		if err == nil {
			t.Fatal("expected error when one hook has timeout and another does not")
		}
		if !strings.Contains(err.Error(), "conflicting timeouts") {
			t.Fatalf("expected conflicting timeouts error, got: %v", err)
		}
	})
}

func TestGenerateCollapsedDomainHooksSameEntrypoint(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	p := New("test-plugin").
		WithDomainHook(ForLibvirt(&stubDomainHandler{})).
		WithDomainHook(ForLibvirt(&stubDomainHandler{}))
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

	var cr testPluginCR
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "plugin.yaml"), &cr)

	if len(cr.Spec.DomainHooks) != 1 {
		t.Fatalf("expected domain hooks with same entrypoint to be collapsed into 1 CR entry, got %d", len(cr.Spec.DomainHooks))
	}

	if cr.Spec.DomainHooks[0].Sidecar.SocketPath != "/var/run/kubevirt-plugin/test-plugin/domain.sock" {
		t.Fatalf("expected default socket path, got %q", cr.Spec.DomainHooks[0].Sidecar.SocketPath)
	}
}

func TestGenerateNodeSocketPathSubdirectory(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	p := New("test-plugin").
		WithNodeHook(PreVMStart, NodeHandler(&stubNodeHandler{}).WithEntrypoint("ep-a"))
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

	var cr testPluginCR
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "plugin.yaml"), &cr)

	expected := "/var/run/kubevirt/plugins/test-plugin/ep-a/node.sock"
	if cr.Spec.NodeHooks[0].Socket != expected {
		t.Fatalf("expected socket path %q, got %q", expected, cr.Spec.NodeHooks[0].Socket)
	}
}
