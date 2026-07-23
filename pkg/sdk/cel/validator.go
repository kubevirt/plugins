package cel

import (
	"fmt"
	"reflect"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/ext"
	v1 "kubevirt.io/api/core/v1"
	"libvirt.org/go/libvirtxml"
)

func ValidateDomainCELExpression(expr string) error {
	env, err := newDomainEnv()
	if err != nil {
		return fmt.Errorf("creating CEL environment: %w", err)
	}
	return validateMutation(env, expr)
}

func ValidateDomainHookCondition(expr string) error {
	env, err := newDomainEnv()
	if err != nil {
		return fmt.Errorf("creating CEL environment: %w", err)
	}
	return validateCondition(env, expr)
}

func ValidateNodeHookCondition(expr string) error {
	env, err := newNodeEnv()
	if err != nil {
		return fmt.Errorf("creating CEL environment: %w", err)
	}
	return validateCondition(env, expr)
}

func EvaluateDomainHookCondition(expr string, vmi *v1.VirtualMachineInstance, domain *libvirtxml.Domain) (bool, error) {
	env, err := newDomainEnv()
	if err != nil {
		return false, fmt.Errorf("creating CEL environment: %w", err)
	}

	return evaluate(env, expr, map[string]any{
		"vmi":        vmi,
		"domainSpec": domain,
	})
}

func EvaluateNodeHookCondition(expr string, vmi *v1.VirtualMachineInstance) (bool, error) {
	env, err := newNodeEnv()
	if err != nil {
		return false, fmt.Errorf("creating CEL environment: %w", err)
	}

	return evaluate(env, expr, map[string]any{
		"vmi": vmi,
	})
}

func newDomainEnv() (*cel.Env, error) {
	return cel.NewEnv(
		ext.NativeTypes(
			reflect.TypeOf(&libvirtxml.Domain{}),
			reflect.TypeOf(&v1.VirtualMachineInstance{}),
		),
		cel.Container("libvirtxml"),
		cel.Variable("vmi", cel.ObjectType("v1.VirtualMachineInstance")),
		cel.Variable("domainSpec", cel.ObjectType("libvirtxml.Domain")),
	)
}

func newNodeEnv() (*cel.Env, error) {
	return cel.NewEnv(
		ext.NativeTypes(
			reflect.TypeOf(&v1.VirtualMachineInstance{}),
			ext.ParseStructTag("json"),
		),
		ext.Strings(),
		ext.Math(),
		ext.Lists(),
		cel.Variable("vmi", cel.ObjectType("v1.VirtualMachineInstance")),
		cel.CrossTypeNumericComparisons(true),
	)
}

func validateCondition(env *cel.Env, expr string) error {
	ast, issues := env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return fmt.Errorf("CEL compilation error: %w", issues.Err())
	}
	if ast == nil {
		return fmt.Errorf("CEL compilation produced nil AST")
	}
	if ast.OutputType() != cel.BoolType {
		return fmt.Errorf("condition must return bool, got %s", ast.OutputType())
	}
	return nil
}

func validateMutation(env *cel.Env, expr string) error {
	ast, issues := env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return fmt.Errorf("CEL compilation error: %w", issues.Err())
	}
	if ast == nil {
		return fmt.Errorf("CEL compilation produced nil AST")
	}
	if !ast.OutputType().IsEquivalentType(cel.ObjectType("libvirtxml.Domain")) {
		return fmt.Errorf("mutation must return Domain, got %s", ast.OutputType())
	}
	return nil
}

func evaluate(env *cel.Env, expr string, vars map[string]any) (bool, error) {
	ast, issues := env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return false, fmt.Errorf("CEL compilation error: %w", issues.Err())
	}

	program, err := env.Program(ast)
	if err != nil {
		return false, fmt.Errorf("CEL program creation error: %w", err)
	}

	out, _, err := program.Eval(vars)
	if err != nil {
		return false, fmt.Errorf("CEL evaluation error: %w", err)
	}

	result, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("CEL expression did not return a boolean, got %T", out.Value())
	}
	return result, nil
}
