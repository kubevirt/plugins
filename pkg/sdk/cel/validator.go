package cel

import (
	"fmt"
	"reflect"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/ext"
	v1 "kubevirt.io/api/core/v1"
	"libvirt.org/go/libvirtxml"
)

const costLimit = 10_000_000

// The ext library versions KubeVirt v1.10's admitter compiles conditions
// with: the highest versions cel-go v0.26.0 gates functions at. Unversioned
// ext.Strings() and friends resolve to the newest library version of
// whatever cel-go a plugin's build resolves, which could let validation
// accept expressions the admitter rejects.
const (
	stringsLibVersion = 4
	mathLibVersion    = 2
	listsLibVersion   = 3
)

// ValidateCondition checks that expr compiles as a hook condition. An empty
// condition always matches, per the Plugin CRD.
func ValidateCondition(expr string) error {
	if expr == "" {
		return nil
	}

	env, err := newConditionEnv()
	if err != nil {
		return fmt.Errorf("creating CEL environment: %w", err)
	}
	return validateCondition(env, expr)
}

// EvaluateCondition evaluates a hook condition against the given VMI for plugin
// authors' unit tests. KubeVirt treats evaluation errors (for example a missing
// map key) as hard failures regardless of the failure strategy, so guard map
// access with `"key" in vmi.Labels`.
func EvaluateCondition(expr string, vmi *v1.VirtualMachineInstance) (bool, error) {
	if expr == "" {
		return true, nil
	}

	env, err := newConditionEnv()
	if err != nil {
		return false, fmt.Errorf("creating CEL environment: %w", err)
	}

	return evaluate(env, expr, map[string]any{
		"vmi": vmi,
	})
}

// ValidateGuestDefinitionExpression checks that expr compiles as a
// GuestDefinition mutation returning a Domain.
func ValidateGuestDefinitionExpression(expr string) error {
	env, err := newMutationEnv()
	if err != nil {
		return fmt.Errorf("creating CEL environment: %w", err)
	}
	return validateMutation(env, expr)
}

func newConditionEnv() (*cel.Env, error) {
	base, err := newBaseEnv()
	if err != nil {
		return nil, err
	}
	return base.Extend(
		ext.NativeTypes(reflect.TypeOf(&v1.VirtualMachineInstance{})),
		cel.Variable("vmi", cel.ObjectType("v1.VirtualMachineInstance")),
	)
}

func newMutationEnv() (*cel.Env, error) {
	base, err := newBaseEnv()
	if err != nil {
		return nil, err
	}
	return base.Extend(
		ext.NativeTypes(
			reflect.TypeOf(&libvirtxml.Domain{}),
			reflect.TypeOf(&v1.VirtualMachineInstance{}),
		),
		cel.Container("libvirtxml"),
		cel.Variable("vmi", cel.ObjectType("v1.VirtualMachineInstance")),
		cel.Variable("domainSpec", cel.ObjectType("libvirtxml.Domain")),
	)
}

func newBaseEnv() (*cel.Env, error) {
	return cel.NewEnv(
		ext.Strings(ext.StringsVersion(stringsLibVersion)),
		ext.Math(ext.MathVersion(mathLibVersion)),
		ext.Lists(ext.ListsVersion(listsLibVersion)),
		cel.CrossTypeNumericComparisons(true),
		cel.EagerlyValidateDeclarations(true),
	)
}

// compileExpr compiles expr in env, mapping compile failures and nil ASTs to errors.
func compileExpr(env *cel.Env, expr string) (*cel.Ast, error) {
	ast, issues := env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("CEL compilation error: %w", issues.Err())
	}
	if ast == nil {
		return nil, fmt.Errorf("CEL compilation produced nil AST")
	}
	return ast, nil
}

func validateCondition(env *cel.Env, expr string) error {
	ast, err := compileExpr(env, expr)
	if err != nil {
		return err
	}
	if ast.OutputType() != cel.BoolType {
		return fmt.Errorf("condition must return bool, got %s", ast.OutputType())
	}
	return nil
}

func validateMutation(env *cel.Env, expr string) error {
	ast, err := compileExpr(env, expr)
	if err != nil {
		return err
	}
	if !ast.OutputType().IsEquivalentType(cel.ObjectType("libvirtxml.Domain")) {
		return fmt.Errorf("mutation must return Domain, got %s", ast.OutputType())
	}
	return nil
}

func evaluate(env *cel.Env, expr string, vars map[string]any) (bool, error) {
	ast, err := compileExpr(env, expr)
	if err != nil {
		return false, err
	}
	if ast.OutputType() != cel.BoolType {
		return false, fmt.Errorf("condition must return bool, got %s", ast.OutputType())
	}

	program, err := env.Program(ast, cel.CostLimit(costLimit))
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
