package plugin

import (
	"encoding/json"
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
			CEL *struct {
				Expression string `yaml:"expression"`
			} `yaml:"cel,omitempty"`
			Sidecar *struct {
				SocketPath string `yaml:"socketPath"`
			} `yaml:"sidecar,omitempty"`
			Condition       string `yaml:"condition,omitempty"`
			FailureStrategy string `yaml:"failureStrategy,omitempty"`
			Timeout         string `yaml:"timeout,omitempty"`
		} `yaml:"domainHooks,omitempty"`
		NodeHooks []struct {
			PermittedHooks  []string `yaml:"permittedHooks"`
			Socket          string   `yaml:"socket"`
			Condition       string   `yaml:"condition,omitempty"`
			FailureStrategy string   `yaml:"failureStrategy,omitempty"`
			Timeout         string   `yaml:"timeout,omitempty"`
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
		ReinvocationPolicy string                `yaml:"reinvocationPolicy"`
		Mutations          []testMAPMutation     `yaml:"mutations,omitempty"`
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

type testMAPMutation struct {
	PatchType          string `yaml:"patchType"`
	ApplyConfiguration struct {
		Expression string `yaml:"expression"`
	} `yaml:"applyConfiguration"`
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

	if len(cr.Spec.NodeHooks[0].PermittedHooks) != 1 || cr.Spec.NodeHooks[0].PermittedHooks[0] != PreVMStart {
		t.Fatalf("expected permittedHooks [%q], got %v", PreVMStart, cr.Spec.NodeHooks[0].PermittedHooks)
	}

	if cr.Spec.NodeHooks[0].Socket != "/var/run/kubevirt/plugins/test-plugin/node.sock" {
		t.Fatalf("expected socket /var/run/kubevirt/plugins/test-plugin/node.sock, got %q", cr.Spec.NodeHooks[0].Socket)
	}

	if len(cr.Spec.DomainHooks) != 0 {
		t.Fatal("expected no domainHooks")
	}
}

func TestGenerateNodeHookMultipleHookPointsSameEntrypoint(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	p := New("test-plugin").
		WithNodeHook(PreVMStart, NodeHandler(&stubNodeHandler{})).
		WithNodeHook(PostVMStop, NodeHandler(&stubNodeHandler{}))
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

	var cr testPluginCR
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "plugin.yaml"), &cr)

	if len(cr.Spec.NodeHooks) != 1 {
		t.Fatalf("expected 1 nodeHook entry (grouped), got %d", len(cr.Spec.NodeHooks))
	}

	if len(cr.Spec.NodeHooks[0].PermittedHooks) != 2 {
		t.Fatalf("expected 2 permittedHooks, got %d", len(cr.Spec.NodeHooks[0].PermittedHooks))
	}

	if cr.Spec.NodeHooks[0].PermittedHooks[0] != PreVMStart {
		t.Fatalf("expected first hook point %q, got %q", PreVMStart, cr.Spec.NodeHooks[0].PermittedHooks[0])
	}

	if cr.Spec.NodeHooks[0].PermittedHooks[1] != PostVMStop {
		t.Fatalf("expected second hook point %q, got %q", PostVMStop, cr.Spec.NodeHooks[0].PermittedHooks[1])
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

	if m.APIVersion != "admissionregistration.k8s.io/v1" {
		t.Fatalf("expected apiVersion admissionregistration.k8s.io/v1, got %q", m.APIVersion)
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

	// Single MAP for all entrypoints
	var m testMAP
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "mutating-admission-policy.yaml"), &m)

	if m.Metadata.Name != "test-plugin" {
		t.Fatalf("expected single MAP named 'test-plugin', got %q", m.Metadata.Name)
	}

	// No per-entrypoint MAP files should exist
	if generatedFileExists(outputDir, "ep-a-mutating-admission-policy.yaml") {
		t.Fatal("expected no per-entrypoint MAP files")
	}

	if generatedFileExists(outputDir, "ep-b-mutating-admission-policy.yaml") {
		t.Fatal("expected no per-entrypoint MAP files")
	}

	// Single binding
	var mb testMAPBinding
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "mutating-admission-policy-binding.yaml"), &mb)

	if mb.Spec.PolicyName != "test-plugin" {
		t.Fatalf("expected binding policyName 'test-plugin', got %q", mb.Spec.PolicyName)
	}

	if mb.Spec.ParamRef.Name != "test-plugin" {
		t.Fatalf("expected paramRef name 'test-plugin', got %q", mb.Spec.ParamRef.Name)
	}

	// No per-entrypoint binding files
	if generatedFileExists(outputDir, "ep-a-mutating-admission-policy-binding.yaml") {
		t.Fatal("expected no per-entrypoint binding files")
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

	if len(m.Spec.Mutations) != 1 {
		t.Fatalf("expected 1 mutation, got %d", len(m.Spec.Mutations))
	}

	if m.Spec.Mutations[0].PatchType != "ApplyConfiguration" {
		t.Fatalf("expected patchType ApplyConfiguration, got %q", m.Spec.Mutations[0].PatchType)
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

func TestGenerateConflictingNodeHooksSameEntrypoint(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	t.Run("conflicting conditions", func(t *testing.T) {
		p := New("test-plugin").
			WithNodeHook(PreVMStart, NodeHandler(&stubNodeHandler{}).WithCondition("vmi.metadata.name == 'a'")).
			WithNodeHook(PostVMStop, NodeHandler(&stubNodeHandler{}).WithCondition("vmi.metadata.name == 'b'"))
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
			WithNodeHook(PreVMStart, NodeHandler(&stubNodeHandler{}).WithTimeout(10 * time.Second)).
			WithNodeHook(PostVMStop, NodeHandler(&stubNodeHandler{}).WithTimeout(30 * time.Second))
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
			WithNodeHook(PreVMStart, NodeHandler(&stubNodeHandler{}).WithFailureStrategy(Fail)).
			WithNodeHook(PostVMStop, NodeHandler(&stubNodeHandler{}).WithFailureStrategy(Ignore))
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
			WithNodeHook(PreVMStart, NodeHandler(&stubNodeHandler{}).WithTimeout(10 * time.Second)).
			WithNodeHook(PostVMStop, NodeHandler(&stubNodeHandler{}))
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

func TestGeneratePluginCRCELDomainHookOnly(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	p := New("test-plugin").WithDomainCELHook("domain.name == 'test'")
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

	var cr testPluginCR
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "plugin.yaml"), &cr)

	if len(cr.Spec.DomainHooks) != 1 {
		t.Fatalf("expected 1 domainHook, got %d", len(cr.Spec.DomainHooks))
	}

	if cr.Spec.DomainHooks[0].CEL == nil {
		t.Fatal("expected CEL domain hook")
	}

	if cr.Spec.DomainHooks[0].CEL.Expression != "domain.name == 'test'" {
		t.Fatalf("expected expression, got %q", cr.Spec.DomainHooks[0].CEL.Expression)
	}

	if cr.Spec.DomainHooks[0].Sidecar != nil {
		t.Fatal("expected no sidecar")
	}
}

func TestGenerateMultipleCELDomainHooks(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	p := New("test-plugin").
		WithDomainCELHook("domain.name == 'a'").
		WithDomainCELHook("domain.name == 'b'")
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

	var cr testPluginCR
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "plugin.yaml"), &cr)

	if len(cr.Spec.DomainHooks) != 2 {
		t.Fatalf("expected 2 domainHooks, got %d", len(cr.Spec.DomainHooks))
	}

	if cr.Spec.DomainHooks[0].CEL == nil || cr.Spec.DomainHooks[0].CEL.Expression != "domain.name == 'a'" {
		t.Fatalf("expected first CEL expression, got %+v", cr.Spec.DomainHooks[0])
	}

	if cr.Spec.DomainHooks[1].CEL == nil || cr.Spec.DomainHooks[1].CEL.Expression != "domain.name == 'b'" {
		t.Fatalf("expected second CEL expression, got %+v", cr.Spec.DomainHooks[1])
	}
}

func TestGenerateCELFirstSidecarSecondOrder(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	p := New("test-plugin").
		WithDomainCELHook("domain.name == 'cel-first'").
		WithDomainHook(ForLibvirt(&stubDomainHandler{}))
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

	var cr testPluginCR
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "plugin.yaml"), &cr)

	if len(cr.Spec.DomainHooks) != 2 {
		t.Fatalf("expected 2 domainHooks, got %d", len(cr.Spec.DomainHooks))
	}

	// CEL should be first (declaration order)
	if cr.Spec.DomainHooks[0].CEL == nil {
		t.Fatal("expected first hook to be CEL (declaration order)")
	}

	// Sidecar should be second
	if cr.Spec.DomainHooks[1].Sidecar == nil {
		t.Fatal("expected second hook to be sidecar (declaration order)")
	}
}

func TestGenerateCELDomainHookWithNodeHook(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	p := New("test-plugin").
		WithDomainCELHook("domain.name == 'test'").
		WithNodeHook(PreVMStart, NodeHandler(&stubNodeHandler{}))
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

	var cr testPluginCR
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "plugin.yaml"), &cr)

	if len(cr.Spec.DomainHooks) != 1 {
		t.Fatalf("expected 1 domainHook, got %d", len(cr.Spec.DomainHooks))
	}

	if cr.Spec.DomainHooks[0].CEL == nil {
		t.Fatal("expected CEL domain hook")
	}

	if len(cr.Spec.NodeHooks) != 1 {
		t.Fatalf("expected 1 nodeHook, got %d", len(cr.Spec.NodeHooks))
	}

	// DaemonSet should be generated (for node hook)
	findGeneratedFile(t, outputDir, "daemonset.yaml")

	// MAP should NOT be generated (no sidecar domain hooks)
	if generatedFileExists(outputDir, "mutating-admission-policy.yaml") {
		t.Fatal("expected no MAP for CEL-only domain hooks")
	}

	// Dockerfile should be generated (node hook needs container)
	if _, err := os.Stat(filepath.Join(sourceDir, "Dockerfile")); os.IsNotExist(err) {
		t.Fatal("expected Dockerfile for node hook plugin")
	}
}

func TestGenerateCELOnlyNoDockerfile(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	p := New("test-plugin").WithDomainCELHook("domain.name == 'test'")
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(sourceDir, "Dockerfile")); !os.IsNotExist(err) {
		t.Fatal("CEL-only plugin should not generate Dockerfile")
	}

	if _, err := os.Stat(filepath.Join(sourceDir, "Makefile")); !os.IsNotExist(err) {
		t.Fatal("CEL-only plugin should not generate Makefile")
	}
}

func TestGenerateCELOnlyNoMAP(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	p := New("test-plugin").WithDomainCELHook("domain.name == 'test'")
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

	if generatedFileExists(outputDir, "mutating-admission-policy.yaml") {
		t.Fatal("CEL-only plugin should not generate MAP")
	}

	if generatedFileExists(outputDir, "mutating-admission-policy-binding.yaml") {
		t.Fatal("CEL-only plugin should not generate MAP binding")
	}
}

func TestGenerateMixedSidecarAndCEL(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	p := New("test-plugin").
		WithDomainHook(ForLibvirt(&stubDomainHandler{})).
		WithDomainCELHook("domain.name == 'mutated'")
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

	var cr testPluginCR
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "plugin.yaml"), &cr)

	if len(cr.Spec.DomainHooks) != 2 {
		t.Fatalf("expected 2 domainHooks, got %d", len(cr.Spec.DomainHooks))
	}

	if cr.Spec.DomainHooks[0].Sidecar == nil {
		t.Fatal("expected first hook to be sidecar")
	}

	if cr.Spec.DomainHooks[1].CEL == nil {
		t.Fatal("expected second hook to be CEL")
	}

	if cr.Spec.DomainHooks[1].CEL.Expression != "domain.name == 'mutated'" {
		t.Fatalf("expected CEL expression, got %q", cr.Spec.DomainHooks[1].CEL.Expression)
	}

	// MAP should still be generated for the sidecar hook
	findGeneratedFile(t, outputDir, "mutating-admission-policy.yaml")
}

func TestGenerateCELWithPerHookSettings(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	timeout := 30 * time.Second
	p := New("test-plugin").WithDomainHook(
		CELDomainHook("domain.name == 'test'").
			WithCondition("vmi.metadata.name == 'my-vmi'").
			WithFailureStrategy(Ignore).
			WithTimeout(timeout),
	)
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

	var cr testPluginCR
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "plugin.yaml"), &cr)

	if len(cr.Spec.DomainHooks) != 1 {
		t.Fatalf("expected 1 domainHook, got %d", len(cr.Spec.DomainHooks))
	}

	hook := cr.Spec.DomainHooks[0]

	if hook.Condition != "vmi.metadata.name == 'my-vmi'" {
		t.Fatalf("expected condition, got %q", hook.Condition)
	}

	if hook.FailureStrategy != "Ignore" {
		t.Fatalf("expected failureStrategy Ignore, got %q", hook.FailureStrategy)
	}

	if hook.Timeout != "30s" {
		t.Fatalf("expected timeout 30s, got %q", hook.Timeout)
	}
}

func TestGenerateCELExpressionWithDoubleQuotes(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	expr := `domain.metadata.annotations["key"] == "value"`
	p := New("test-plugin").WithDomainCELHook(expr)
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

	content := readFileContent(t, findGeneratedFile(t, outputDir, "plugin.yaml"))

	var cr testPluginCR
	if err := yaml.Unmarshal([]byte(content), &cr); err != nil {
		t.Fatalf("generated YAML with double-quoted expression is invalid: %v\nContent:\n%s", err, content)
	}

	if cr.Spec.DomainHooks[0].CEL.Expression != expr {
		t.Fatalf("expected expression to round-trip, got %q", cr.Spec.DomainHooks[0].CEL.Expression)
	}
}

func TestCelStringLiteral(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple", `hello`, `"hello"`},
		{"with double quotes", `say "hi"`, `"say \"hi\""`},
		{"with backslash", `path\to`, `"path\\to"`},
		{"backslash before quote", `a\"b`, `"a\\\"b"`},
		{"json payload", `[{"image":"test:latest"}]`, `"[{\"image\":\"test:latest\"}]"`},
		{"empty", ``, `""`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := celStringLiteral(tt.input)
			if result != tt.expected {
				t.Fatalf("celStringLiteral(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestGenerateMAPMutations(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	p := New("test-plugin").WithDomainHook(ForLibvirt(&stubDomainHandler{}))
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

	mapFile := findGeneratedFile(t, outputDir, "mutating-admission-policy.yaml")
	content := readFileContent(t, mapFile)

	if !strings.Contains(content, "mutations:") {
		t.Fatalf("expected MAP to contain mutations field, got:\n%s", content)
	}

	if !strings.Contains(content, "patchType: ApplyConfiguration") {
		t.Fatalf("expected patchType ApplyConfiguration, got:\n%s", content)
	}

	if !strings.Contains(content, "hooks.kubevirt.io/hookSidecars") {
		t.Fatalf("expected hookSidecars annotation in expression, got:\n%s", content)
	}

	if !strings.Contains(content, "quay.io/myorg/test-plugin:latest") {
		t.Fatalf("expected default image in sidecar annotation, got:\n%s", content)
	}
}

func TestGenerateMAPMutationsWithCustomImage(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	p := New("test-plugin").
		WithImage("registry.example.com/my-plugin:v1.0").
		WithDomainHook(ForLibvirt(&stubDomainHandler{}))
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

	content := readFileContent(t, findGeneratedFile(t, outputDir, "mutating-admission-policy.yaml"))

	if !strings.Contains(content, "registry.example.com/my-plugin:v1.0") {
		t.Fatalf("expected custom image in MAP mutations, got:\n%s", content)
	}

	if strings.Contains(content, "quay.io/myorg") {
		t.Fatalf("expected no default image when custom image set, got:\n%s", content)
	}
}

func TestGenerateMAPMutationsMultiEntrypoint(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	p := New("test-plugin").
		WithDomainHook(ForLibvirt(&stubDomainHandler{}).WithEntrypoint("ep-a")).
		WithDomainHook(ForLibvirt(&stubDomainHandler{}).WithEntrypoint("ep-b"))
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

	content := readFileContent(t, findGeneratedFile(t, outputDir, "mutating-admission-policy.yaml"))

	if !strings.Contains(content, "--entrypoint") {
		t.Fatalf("expected --entrypoint args in multi-entrypoint MAP, got:\n%s", content)
	}

	if !strings.Contains(content, "ep-a") {
		t.Fatalf("expected ep-a in MAP mutations, got:\n%s", content)
	}

	if !strings.Contains(content, "ep-b") {
		t.Fatalf("expected ep-b in MAP mutations, got:\n%s", content)
	}
}

func TestGenerateMAPMutationsJSONRoundTrip(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	p := New("test-plugin").
		WithImage("registry.example.com/my-plugin:v1.0").
		WithImagePullPolicy("Always").
		WithDomainHook(ForLibvirt(&stubDomainHandler{}).WithEntrypoint("ep-a")).
		WithDomainHook(ForLibvirt(&stubDomainHandler{}).WithEntrypoint("ep-b"))
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

	content := readFileContent(t, findGeneratedFile(t, outputDir, "mutating-admission-policy.yaml"))

	// Extract the CEL string value from the expression
	// The annotation value is between the first and last escaped quotes on the hookSidecars line
	lines := strings.Split(content, "\n")
	var annotationLine string
	for _, line := range lines {
		if strings.Contains(line, "hooks.kubevirt.io/hookSidecars") {
			annotationLine = strings.TrimSpace(line)
			break
		}
	}

	if annotationLine == "" {
		t.Fatal("could not find hookSidecars annotation line")
	}

	// Extract JSON from CEL: the line looks like:
	// "hooks.kubevirt.io/hookSidecars": "[{\"image\":\"...\"}]"
	// Find the second ": " and extract the CEL string value
	colonIdx := strings.Index(annotationLine, `": `)
	if colonIdx < 0 {
		t.Fatalf("could not find colon separator in annotation line: %s", annotationLine)
	}
	celValue := strings.TrimSpace(annotationLine[colonIdx+3:])

	// Remove surrounding quotes
	if len(celValue) < 2 || celValue[0] != '"' || celValue[len(celValue)-1] != '"' {
		t.Fatalf("expected quoted CEL string, got: %s", celValue)
	}
	escaped := celValue[1 : len(celValue)-1]

	// Reverse CEL escaping: \" -> " and \\ -> \
	jsonStr := strings.ReplaceAll(escaped, `\"`, `"`)
	jsonStr = strings.ReplaceAll(jsonStr, `\\`, `\`)

	// Parse JSON
	var sidecars []hookSidecar
	if err := json.Unmarshal([]byte(jsonStr), &sidecars); err != nil {
		t.Fatalf("sidecar annotation is not valid JSON after unescaping: %v\nJSON: %s\nFull CEL value: %s", err, jsonStr, celValue)
	}

	if len(sidecars) != 2 {
		t.Fatalf("expected 2 sidecars, got %d: %+v", len(sidecars), sidecars)
	}

	for _, sc := range sidecars {
		if sc.Image != "registry.example.com/my-plugin:v1.0" {
			t.Fatalf("expected custom image, got %q", sc.Image)
		}
		if sc.ImagePullPolicy != "Always" {
			t.Fatalf("expected imagePullPolicy Always, got %q", sc.ImagePullPolicy)
		}
	}

	// First sidecar should have --entrypoint ep-a
	if len(sidecars[0].Args) != 2 || sidecars[0].Args[1] != "ep-a" {
		t.Fatalf("expected first sidecar args [--entrypoint ep-a], got %v", sidecars[0].Args)
	}

	// Second sidecar should have --entrypoint ep-b
	if len(sidecars[1].Args) != 2 || sidecars[1].Args[1] != "ep-b" {
		t.Fatalf("expected second sidecar args [--entrypoint ep-b], got %v", sidecars[1].Args)
	}
}

func TestGenerateDaemonSetWithCustomImage(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	p := New("test-plugin").
		WithImage("registry.example.com/my-plugin:v1.0").
		WithNodeHook(PreVMStart, NodeHandler(&stubNodeHandler{}))
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

	var ds testDaemonSet
	readAndUnmarshal(t, findGeneratedFile(t, outputDir, "daemonset.yaml"), &ds)

	container := ds.Spec.Template.Spec.Containers[0]
	if container.Image != "registry.example.com/my-plugin:v1.0" {
		t.Fatalf("expected custom image, got %q", container.Image)
	}
}

func TestGenerateDaemonSetWithImagePullPolicy(t *testing.T) {
	sourceDir := setupSourceDir(t)
	outputDir := filepath.Join(t.TempDir(), "deploy")

	p := New("test-plugin").
		WithImage("registry.example.com/my-plugin:v1.0").
		WithImagePullPolicy("Always").
		WithNodeHook(PreVMStart, NodeHandler(&stubNodeHandler{}))
	if err := p.generate(outputDir, sourceDir); err != nil {
		t.Fatal(err)
	}

	content := readFileContent(t, findGeneratedFile(t, outputDir, "daemonset.yaml"))

	if !strings.Contains(content, "imagePullPolicy: Always") {
		t.Fatalf("expected imagePullPolicy Always in DaemonSet, got:\n%s", content)
	}
}
