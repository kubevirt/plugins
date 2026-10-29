package cel

import (
	"strings"
	"testing"

	"github.com/kubevirt/plugins/pkg/sdk/testing/fixtures"
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
// gated at the pinned versions (strings 4, math 2, lists 2) must still
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

func TestValidateConditionRejectsJSONStyleFieldNames(t *testing.T) {
	invalidExprs := []string{
		"vmi.metadata.name == 'test'",
		"vmi.spec.domain.cpu.cores > 0",
	}

	for _, expr := range invalidExprs {
		if err := ValidateCondition(expr); err == nil {
			t.Errorf("expected JSON-style expression %q to fail validation", expr)
		}
	}
}

func TestValidateConditionRejectsDomainSpec(t *testing.T) {
	err := ValidateCondition("domainSpec.Name == 'x'")

	if err == nil {
		t.Error("expected domainSpec to be rejected in conditions")
	}
}

func TestValidateConditionRejectsInvalidSyntax(t *testing.T) {
	err := ValidateCondition("vmi.Name ==")

	if err == nil {
		t.Error("expected invalid syntax to fail validation")
	}
}

func TestValidateConditionRejectsUnknownField(t *testing.T) {
	err := ValidateCondition("vmi.Spec.Doman.CPU.Cores > 2")

	if err == nil {
		t.Error("expected unknown field to be rejected")
	}
}

func TestValidateConditionRejectsNonBool(t *testing.T) {
	err := ValidateCondition("vmi.Name")

	if err == nil {
		t.Error("expected non-bool return type to be rejected for condition expression")
	}
}

func TestEvaluateConditionTrue(t *testing.T) {
	vmi := fixtures.BasicVMI()

	result, err := EvaluateCondition("vmi.Name == 'test-vmi'", vmi)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result {
		t.Error("expected condition to evaluate to true")
	}
}

func TestEvaluateConditionFalse(t *testing.T) {
	vmi := fixtures.BasicVMI()

	result, err := EvaluateCondition("vmi.Name == 'other'", vmi)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result {
		t.Error("expected condition to evaluate to false")
	}
}

func TestEvaluateConditionNestedField(t *testing.T) {
	vmi := fixtures.BasicVMI()

	result, err := EvaluateCondition("vmi.Spec.Domain.CPU.Cores > 1", vmi)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result {
		t.Error("expected condition to evaluate to true")
	}
}

func TestEvaluateConditionMissingMapKeyFails(t *testing.T) {
	vmi := fixtures.BasicVMI()
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
	vmi := fixtures.BasicVMI()
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
	vmi := fixtures.BasicVMI()

	result, err := EvaluateCondition("", vmi)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result {
		t.Error("expected empty condition to evaluate to true")
	}
}

func TestEvaluateConditionInvalidExpr(t *testing.T) {
	vmi := fixtures.BasicVMI()

	_, err := EvaluateCondition("vmi.Name ==", vmi)
	if err == nil {
		t.Fatal("expected error for invalid CEL expression")
	}

	if !strings.Contains(err.Error(), "CEL compilation error") {
		t.Fatalf("expected CEL compilation error, got: %v", err)
	}
}

func TestEvaluateConditionNonBool(t *testing.T) {
	vmi := fixtures.BasicVMI()

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

func TestValidateGuestDefinitionExpressionRejectsNonDomain(t *testing.T) {
	invalidExprs := []string{
		"true",
		`"not a domain"`,
		"vmi.Name",
	}

	for _, expr := range invalidExprs {
		if err := ValidateGuestDefinitionExpression(expr); err == nil {
			t.Errorf("expected non-Domain expression %q to be rejected", expr)
		}
	}
}

func TestValidateGuestDefinitionExpressionRejectsUnknownField(t *testing.T) {
	err := ValidateGuestDefinitionExpression(`Domain{Bogus: "x"}`)

	if err == nil {
		t.Error("expected unknown field to be rejected")
	}
}

func TestValidateGuestDefinitionExpressionRejectsJSONStyleFieldNames(t *testing.T) {
	invalidExprs := []string{
		`Domain{Title: vmi.metadata.name}`,
		`Domain{title: "x"}`,
	}

	for _, expr := range invalidExprs {
		if err := ValidateGuestDefinitionExpression(expr); err == nil {
			t.Errorf("expected JSON-style expression %q to be rejected", expr)
		}
	}
}

func TestValidateGuestDefinitionExpressionRejectsInvalidSyntax(t *testing.T) {
	if err := ValidateGuestDefinitionExpression("invalid >>><< expression"); err == nil {
		t.Fatal("expected error for invalid CEL expression")
	}
}
