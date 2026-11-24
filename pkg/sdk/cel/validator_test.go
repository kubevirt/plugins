package cel

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "kubevirt.io/api/core/v1"
)

func TestValidateConditionAcceptsGoFieldNames(t *testing.T) {
	validExprs := []string{
		"vmi.Name == 'test-vmi'",
		"vmi.Namespace == 'default'",
		`"app" in vmi.Labels`,
		"vmi.Spec.Domain.CPU.Cores > 2",
	}

	for _, expr := range validExprs {
		if err := ValidateCondition(expr); err != nil {
			t.Errorf("expected valid expression %q to pass, got: %v", expr, err)
		}
	}
}

func TestValidateConditionAcceptsEmpty(t *testing.T) {
	if err := ValidateCondition(""); err != nil {
		t.Errorf("expected empty condition to pass, got: %v", err)
	}
}

func TestValidateConditionAcceptsExtFunctions(t *testing.T) {
	validExprs := []string{
		"vmi.Name.lowerAscii() == 'test-vmi'",
		"math.greatest(1, 2) == 2",
		"size(lists.range(3)) == 3",
	}

	for _, expr := range validExprs {
		if err := ValidateCondition(expr); err != nil {
			t.Errorf("expected valid expression %q to pass, got: %v", expr, err)
		}
	}
}

// TestValidateConditionAcceptsHighestPinnedExtVersions asserts the ext
// library version pins reproduce cel-go v0.26.0's unversioned defaults,
// which KubeVirt v1.10's admitter compiles conditions with: every function
// gated at the pinned versions (strings 4, math 2, lists 3) must still
// compile. The pins keep a newer cel-go, resolved into a plugin's build
// through MVS, from adding higher-version functions to the validation
// environment that the admitter would reject; no such function exists as of
// cel-go v0.32.0, so this positive assertion is the strongest provable one.
func TestValidateConditionAcceptsHighestPinnedExtVersions(t *testing.T) {
	validExprs := []string{
		`"%s".format(["pinned"]) == "pinned"`, // strings version 4
		"math.sqrt(4.0) == 2.0",               // math version 2
		"[3, 1, 2].sort() == [1, 2, 3]",       // lists version 2
	}

	for _, expr := range validExprs {
		if err := ValidateCondition(expr); err != nil {
			t.Errorf("expected pinned-version expression %q to pass, got: %v", expr, err)
		}
	}
}

func TestValidateConditionRejects(t *testing.T) {
	invalidExprs := []struct {
		expr   string
		reason string
	}{
		{"vmi.metadata.name == 'test'", "JSON-style field name"},
		{"vmi.spec.domain.cpu.cores > 0", "JSON-style field name"},
		{"domainSpec.Name == 'x'", "domainSpec is not available in conditions"},
		{"vmi.Name ==", "invalid syntax"},
		{"vmi.Spec.Doman.CPU.Cores > 2", "unknown field"},
		{"vmi.Name", "non-bool result"},
	}

	for _, tc := range invalidExprs {
		if err := ValidateCondition(tc.expr); err == nil {
			t.Errorf("expected expression %q to be rejected (%s)", tc.expr, tc.reason)
		}
	}
}

// basicVMI returns a VMI with the fields the evaluated expressions read.
func basicVMI() *v1.VirtualMachineInstance {
	return &v1.VirtualMachineInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "test-vmi"},
		Spec:       v1.VirtualMachineInstanceSpec{Domain: v1.DomainSpec{CPU: &v1.CPU{Cores: 2}}},
	}
}

func TestEvaluateConditionTrue(t *testing.T) {
	vmi := basicVMI()

	result, err := EvaluateCondition("vmi.Name == 'test-vmi'", vmi)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result {
		t.Error("expected condition to evaluate to true")
	}
}

func TestEvaluateConditionFalse(t *testing.T) {
	vmi := basicVMI()

	result, err := EvaluateCondition("vmi.Name == 'other'", vmi)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result {
		t.Error("expected condition to evaluate to false")
	}
}

func TestEvaluateConditionNestedField(t *testing.T) {
	vmi := basicVMI()

	result, err := EvaluateCondition("vmi.Spec.Domain.CPU.Cores > 1", vmi)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result {
		t.Error("expected condition to evaluate to true")
	}
}

func TestEvaluateConditionMissingMapKeyFails(t *testing.T) {
	vmi := basicVMI()
	vmi.Labels = map[string]string{"app": "test"}

	_, err := EvaluateCondition(`vmi.Labels["missing"] == "v"`, vmi)
	if err == nil {
		t.Fatal("expected error for missing map key")
	}

	if !strings.Contains(err.Error(), "no such key") {
		t.Fatalf("expected 'no such key' error, got: %v", err)
	}
}

func TestEvaluateConditionGuardedMapAccess(t *testing.T) {
	vmi := basicVMI()
	vmi.Labels = map[string]string{"app": "test"}

	result, err := EvaluateCondition(`"app" in vmi.Labels && vmi.Labels["app"] == "test"`, vmi)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result {
		t.Error("expected guarded condition to evaluate to true")
	}
}

func TestEvaluateConditionEmptyReturnsTrue(t *testing.T) {
	vmi := basicVMI()

	result, err := EvaluateCondition("", vmi)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result {
		t.Error("expected empty condition to evaluate to true")
	}
}

func TestEvaluateConditionInvalidExpr(t *testing.T) {
	vmi := basicVMI()

	_, err := EvaluateCondition("vmi.Name ==", vmi)
	if err == nil {
		t.Fatal("expected error for invalid CEL expression")
	}

	if !strings.Contains(err.Error(), "CEL compilation error") {
		t.Fatalf("expected CEL compilation error, got: %v", err)
	}
}

func TestEvaluateConditionNonBool(t *testing.T) {
	vmi := basicVMI()

	_, err := EvaluateCondition("vmi.Name", vmi)
	if err == nil {
		t.Fatal("expected error for non-boolean CEL expression")
	}

	if !strings.Contains(err.Error(), "condition must return bool") {
		t.Fatalf("expected 'condition must return bool' error, got: %v", err)
	}
}

func TestValidateGuestDefinitionExpressionValid(t *testing.T) {
	validExprs := []string{
		"Domain{}",
		`Domain{Title: "x"}`,
		`Domain{Memory: DomainMemory{Value: uint(2048), Unit: "GiB"}}`,
		`Domain{Title: has(domainSpec.Memory) ? "memory-present" : "memory-absent"}`,
		`Domain{Title: vmi.Name}`,
	}

	for _, expr := range validExprs {
		if err := ValidateGuestDefinitionExpression(expr); err != nil {
			t.Fatalf("expected valid expression %q, got error: %v", expr, err)
		}
	}
}

func TestValidateGuestDefinitionExpressionRejects(t *testing.T) {
	invalidExprs := []struct {
		expr   string
		reason string
	}{
		{"true", "non-Domain result"},
		{`"not a domain"`, "non-Domain result"},
		{"vmi.Name", "non-Domain result"},
		{`Domain{Bogus: "x"}`, "unknown field"},
		{`Domain{Title: vmi.metadata.name}`, "JSON-style field name"},
		{`Domain{title: "x"}`, "JSON-style field name"},
		{"invalid >>><< expression", "invalid syntax"},
	}

	for _, tc := range invalidExprs {
		if err := ValidateGuestDefinitionExpression(tc.expr); err == nil {
			t.Errorf("expected expression %q to be rejected (%s)", tc.expr, tc.reason)
		}
	}
}
