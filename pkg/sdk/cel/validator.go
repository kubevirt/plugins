package cel

import (
	"encoding/json"
	"fmt"

	"github.com/google/cel-go/cel"
	v1 "kubevirt.io/api/core/v1"
)

func ValidateDomainHookCondition(expr string) error {
	env, err := newDomainEnv()
	if err != nil {
		return fmt.Errorf("creating CEL environment: %w", err)
	}
	return validateExpr(env, expr)
}

func ValidateNodeHookCondition(expr string) error {
	env, err := newNodeEnv()
	if err != nil {
		return fmt.Errorf("creating CEL environment: %w", err)
	}
	return validateExpr(env, expr)
}

func EvaluateDomainHookCondition(expr string, vmi *v1.VirtualMachineInstance, domainSpec *v1.DomainSpec) (bool, error) {
	env, err := newDomainEnv()
	if err != nil {
		return false, fmt.Errorf("creating CEL environment: %w", err)
	}

	vmiMap, err := toMap(vmi)
	if err != nil {
		return false, fmt.Errorf("converting VMI: %w", err)
	}
	domainSpecMap, err := toMap(domainSpec)
	if err != nil {
		return false, fmt.Errorf("converting DomainSpec: %w", err)
	}

	return evaluate(env, expr, map[string]any{
		"vmi":        vmiMap,
		"domainSpec": domainSpecMap,
	})
}

func EvaluateNodeHookCondition(expr string, vmi *v1.VirtualMachineInstance) (bool, error) {
	env, err := newNodeEnv()
	if err != nil {
		return false, fmt.Errorf("creating CEL environment: %w", err)
	}

	vmiMap, err := toMap(vmi)
	if err != nil {
		return false, fmt.Errorf("converting VMI: %w", err)
	}

	return evaluate(env, expr, map[string]any{
		"vmi": vmiMap,
	})
}

func newDomainEnv() (*cel.Env, error) {
	return cel.NewEnv(
		cel.Variable("vmi", cel.DynType),
		cel.Variable("domainSpec", cel.DynType),
	)
}

func newNodeEnv() (*cel.Env, error) {
	return cel.NewEnv(
		cel.Variable("vmi", cel.DynType),
	)
}

func validateExpr(env *cel.Env, expr string) error {
	ast, issues := env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return fmt.Errorf("CEL compilation error: %w", issues.Err())
	}
	if ast == nil {
		return fmt.Errorf("CEL compilation produced nil AST")
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

func toMap(obj any) (map[string]any, error) {
	data, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}
