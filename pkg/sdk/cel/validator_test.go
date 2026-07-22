package cel

import (
	"strings"
	"testing"

	"github.com/iholder101/kubevirt-plugins/pkg/sdk/testing/fixtures"
)

func TestValidateDomainHookConditionValidExpr(t *testing.T) {
	err := ValidateDomainHookCondition("domainSpec.Type == 'kvm'")

	if err != nil {
		t.Errorf("expected valid expression to pass, got: %v", err)
	}
}

func TestValidateDomainHookConditionInvalidSyntax(t *testing.T) {
	err := ValidateDomainHookCondition("domainSpec.Type >")

	if err == nil {
		t.Error("expected invalid syntax to fail validation")
	}
}

func TestValidateDomainHookConditionAcceptsDomainSpec(t *testing.T) {
	err := ValidateDomainHookCondition("domainSpec.Name == 'x'")

	if err != nil {
		t.Errorf("expected domainSpec to be available in domain hooks, got: %v", err)
	}
}

func TestValidateDomainHookConditionRejectsInvalidField(t *testing.T) {
	err := ValidateDomainHookCondition("domainSpec.nonexistent == true")

	if err == nil {
		t.Error("expected nonexistent field to be rejected by NativeTypes")
	}
}

func TestValidateNodeHookConditionValidExpr(t *testing.T) {
	err := ValidateNodeHookCondition("vmi.metadata.name == 'test'")

	if err != nil {
		t.Errorf("expected valid expression to pass, got: %v", err)
	}
}

func TestValidateNodeHookConditionInvalidSyntax(t *testing.T) {
	err := ValidateNodeHookCondition("vmi.metadata.name ==")

	if err == nil {
		t.Error("expected invalid syntax to fail validation")
	}
}

func TestValidateNodeHookConditionRejectsDomainSpec(t *testing.T) {
	err := ValidateNodeHookCondition("domainSpec.Name == 'x'")

	if err == nil {
		t.Error("expected domainSpec to be rejected in node hook conditions")
	}
}

func TestEvaluateDomainHookConditionTrue(t *testing.T) {
	vmi := fixtures.BasicVMI()
	domain := fixtures.BasicLibvirtDomain()

	result, err := EvaluateDomainHookCondition("vmi.Name == 'test-vmi'", vmi, domain)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result {
		t.Error("expected condition to evaluate to true")
	}
}

func TestEvaluateDomainHookConditionFalse(t *testing.T) {
	vmi := fixtures.BasicVMI()
	domain := fixtures.BasicLibvirtDomain()

	result, err := EvaluateDomainHookCondition("vmi.Name == 'other'", vmi, domain)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result {
		t.Error("expected condition to evaluate to false")
	}
}

func TestEvaluateNodeHookConditionTrue(t *testing.T) {
	vmi := fixtures.BasicVMI()

	result, err := EvaluateNodeHookCondition("vmi.metadata.name == 'test-vmi'", vmi)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result {
		t.Error("expected condition to evaluate to true")
	}
}

func TestEvaluateNodeHookConditionFalse(t *testing.T) {
	vmi := fixtures.BasicVMI()

	result, err := EvaluateNodeHookCondition("vmi.metadata.name == 'other'", vmi)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result {
		t.Error("expected condition to evaluate to false")
	}
}

func TestNonBooleanCELExpression(t *testing.T) {
	vmi := fixtures.BasicVMI()
	domain := fixtures.BasicLibvirtDomain()

	_, err := EvaluateDomainHookCondition("vmi.Name", vmi, domain)
	if err == nil {
		t.Fatal("expected error for non-boolean CEL expression")
	}

	if !strings.Contains(err.Error(), "did not return a boolean") {
		t.Fatalf("expected 'did not return a boolean' error, got: %v", err)
	}
}

func TestEvaluateDomainHookConditionInvalidExpr(t *testing.T) {
	vmi := fixtures.BasicVMI()
	domain := fixtures.BasicLibvirtDomain()

	_, err := EvaluateDomainHookCondition("vmi.Name ==", vmi, domain)
	if err == nil {
		t.Fatal("expected error for invalid CEL expression")
	}

	if !strings.Contains(err.Error(), "CEL compilation error") {
		t.Fatalf("expected CEL compilation error, got: %v", err)
	}
}

func TestValidateDomainCELExpressionValid(t *testing.T) {
	validExprs := []string{
		"Domain{Name: 'test'}",
	}

	for _, expr := range validExprs {
		if err := ValidateDomainCELExpression(expr); err != nil {
			t.Fatalf("expected valid expression %q, got error: %v", expr, err)
		}
	}
}

func TestValidateDomainCELExpressionInvalid(t *testing.T) {
	if err := ValidateDomainCELExpression("invalid >>><< expression"); err == nil {
		t.Fatal("expected error for invalid CEL expression")
	}
}

func TestEvaluateDomainHookConditionWithDomainSpec(t *testing.T) {
	vmi := fixtures.BasicVMI()
	domain := fixtures.BasicLibvirtDomain()

	result, err := EvaluateDomainHookCondition("domainSpec.Type == 'kvm'", vmi, domain)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result {
		t.Error("expected condition to evaluate to true")
	}
}

func TestNodeEvaluatorRejectsGoFieldNames(t *testing.T) {
	err := ValidateNodeHookCondition("vmi.Name == 'test'")
	if err == nil {
		t.Error("expected Go field name 'vmi.Name' to be rejected in node evaluator (uses JSON tag names)")
	}
}

func TestValidateDomainCELExpressionRejectsNonDomain(t *testing.T) {
	err := ValidateDomainCELExpression("true")
	if err == nil {
		t.Error("expected non-Domain return type to be rejected for mutation expression")
	}
}

func TestValidateDomainHookConditionRejectsNonBool(t *testing.T) {
	err := ValidateDomainHookCondition("Domain{Name: 'test'}")
	if err == nil {
		t.Error("expected non-bool return type to be rejected for condition expression")
	}
}
