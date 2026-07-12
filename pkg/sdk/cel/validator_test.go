package cel

import (
	"strings"
	"testing"

	"github.com/iholder101/kubevirt-plugins/pkg/sdk/testing/fixtures"
)

func TestValidateDomainHookConditionValidExpr(t *testing.T) {
	err := ValidateDomainHookCondition("vmi.spec.domain.cpu.cores > 1")

	if err != nil {
		t.Errorf("expected valid expression to pass, got: %v", err)
	}
}

func TestValidateDomainHookConditionInvalidSyntax(t *testing.T) {
	err := ValidateDomainHookCondition("vmi.spec.domain.cpu.cores >")

	if err == nil {
		t.Error("expected invalid syntax to fail validation")
	}
}

func TestValidateDomainHookConditionAcceptsDomainSpec(t *testing.T) {
	err := ValidateDomainHookCondition("domainSpec.name == 'x'")

	if err != nil {
		t.Errorf("expected domainSpec to be available in domain hooks, got: %v", err)
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
	err := ValidateNodeHookCondition("domainSpec.name == 'x'")

	if err == nil {
		t.Error("expected domainSpec to be rejected in node hook conditions")
	}
}

func TestEvaluateDomainHookConditionTrue(t *testing.T) {
	vmi := fixtures.BasicVMI()

	result, err := EvaluateDomainHookCondition("vmi.spec.domain.cpu.cores == 2", vmi, &vmi.Spec.Domain)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result {
		t.Error("expected condition to evaluate to true")
	}
}

func TestEvaluateDomainHookConditionFalse(t *testing.T) {
	vmi := fixtures.BasicVMI()

	result, err := EvaluateDomainHookCondition("vmi.spec.domain.cpu.cores == 99", vmi, &vmi.Spec.Domain)
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

	_, err := EvaluateDomainHookCondition("vmi.metadata.name", vmi, &vmi.Spec.Domain)
	if err == nil {
		t.Fatal("expected error for non-boolean CEL expression")
	}

	if !strings.Contains(err.Error(), "did not return a boolean") {
		t.Fatalf("expected 'did not return a boolean' error, got: %v", err)
	}
}

func TestEvaluateDomainHookConditionInvalidExpr(t *testing.T) {
	vmi := fixtures.BasicVMI()

	_, err := EvaluateDomainHookCondition("vmi.metadata.name ==", vmi, &vmi.Spec.Domain)
	if err == nil {
		t.Fatal("expected error for invalid CEL expression")
	}

	if !strings.Contains(err.Error(), "CEL compilation error") {
		t.Fatalf("expected CEL compilation error, got: %v", err)
	}
}
