package plugin

import (
	"fmt"
	"time"

	"github.com/iholder101/kubevirt-plugins/pkg/sdk/cel"
	rbacv1 "k8s.io/api/rbac/v1"
)

type Plugin struct {
	name            string
	domainHooks     []DomainHookOption
	nodeHooks       []NodeHookOption
	condition       string
	failureStrategy FailureStrategy
	namespace       string
	rbacRules       []rbacv1.PolicyRule
}

type DomainHookOption struct {
	handler         LibvirtDomainHookHandler
	expression      string
	condition       string
	failureStrategy *FailureStrategy
	timeout         *time.Duration
	entrypoint      string
}

func (o *DomainHookOption) isCEL() bool {
	return o.expression != ""
}

type NodeHookOption struct {
	hookPoint       string
	handler         NodeHookHandler
	condition       string
	failureStrategy *FailureStrategy
	timeout         *time.Duration
	entrypoint      string
}

func New(name string) *Plugin {
	if name == "" {
		panic("plugin name must not be empty")
	}
	validateName(name)
	return &Plugin{name: name, namespace: "default"}
}

func (p *Plugin) WithDomainHook(opt *DomainHookOption) *Plugin {
	if opt.handler == nil && opt.expression == "" {
		panic("domain hook must have either a handler or a CEL expression")
	}
	if opt.handler != nil && opt.expression != "" {
		panic("domain hook must have either a handler or a CEL expression, not both")
	}

	p.domainHooks = append(p.domainHooks, *opt)
	return p
}

func (p *Plugin) WithNodeHook(hookPoint string, opt *NodeHookOption) *Plugin {
	valid := false
	for _, hp := range AllHookPoints() {
		if hp == hookPoint {
			valid = true
			break
		}
	}
	if !valid {
		panic(fmt.Sprintf("invalid hook point %q; valid hook points: %v", hookPoint, AllHookPoints()))
	}
	resolvedEntrypoint := p.resolveEntrypoint(opt.entrypoint)
	for _, existingHook := range p.nodeHooks {
		if existingHook.hookPoint == hookPoint && p.resolveEntrypoint(existingHook.entrypoint) == resolvedEntrypoint {
			panic(fmt.Sprintf("duplicate hook point %q for entrypoint %q; each hook point can only be registered once per entrypoint", hookPoint, resolvedEntrypoint))
		}
	}

	hookCopy := *opt
	hookCopy.hookPoint = hookPoint
	p.nodeHooks = append(p.nodeHooks, hookCopy)
	return p
}

func (p *Plugin) WithCondition(condition string) *Plugin {
	if err := cel.ValidateDomainHookCondition(condition); err != nil {
		panic(fmt.Sprintf("invalid plugin-level CEL condition: %v", err))
	}
	p.condition = condition
	return p
}

func (p *Plugin) WithFailureStrategy(strategy FailureStrategy) *Plugin {
	p.failureStrategy = strategy
	return p
}

func (p *Plugin) WithNamespace(namespace string) *Plugin {
	if namespace == "" {
		panic("namespace must not be empty")
	}
	p.namespace = namespace
	return p
}

func ForLibvirt(handler LibvirtDomainHookHandler) *DomainHookOption {
	return &DomainHookOption{handler: handler}
}

func CELDomainHook(expression string) *DomainHookOption {
	if expression == "" {
		panic("CEL domain hook expression must not be empty")
	}
	if err := cel.ValidateDomainCELExpression(expression); err != nil {
		panic(fmt.Sprintf("invalid CEL domain hook expression: %v", err))
	}
	return &DomainHookOption{expression: expression}
}

func (p *Plugin) WithDomainCELHook(expression string) *Plugin {
	return p.WithDomainHook(CELDomainHook(expression))
}

func (o *DomainHookOption) WithCondition(condition string) *DomainHookOption {
	if err := cel.ValidateDomainHookCondition(condition); err != nil {
		panic(fmt.Sprintf("invalid domain hook CEL condition: %v", err))
	}
	o.condition = condition
	return o
}

func (o *DomainHookOption) WithFailureStrategy(strategy FailureStrategy) *DomainHookOption {
	o.failureStrategy = &strategy
	return o
}

func (o *DomainHookOption) WithTimeout(timeout time.Duration) *DomainHookOption {
	o.timeout = &timeout
	return o
}

func NodeHandler(handler NodeHookHandler) *NodeHookOption {
	return &NodeHookOption{handler: handler}
}

func (o *NodeHookOption) WithCondition(condition string) *NodeHookOption {
	if err := cel.ValidateNodeHookCondition(condition); err != nil {
		panic(fmt.Sprintf("invalid node hook CEL condition: %v", err))
	}
	o.condition = condition
	return o
}

func (o *NodeHookOption) WithFailureStrategy(strategy FailureStrategy) *NodeHookOption {
	o.failureStrategy = &strategy
	return o
}

func (o *NodeHookOption) WithTimeout(timeout time.Duration) *NodeHookOption {
	o.timeout = &timeout
	return o
}

func (o *DomainHookOption) WithEntrypoint(name string) *DomainHookOption {
	if o.isCEL() {
		panic("CEL domain hooks do not support entrypoints")
	}
	validateEntrypoint(name)
	o.entrypoint = name
	return o
}

func (o *NodeHookOption) WithEntrypoint(name string) *NodeHookOption {
	validateEntrypoint(name)
	o.entrypoint = name
	return o
}

func validateName(name string) {
	validateK8sName("plugin name", name)
}

func validateEntrypoint(name string) {
	if name == "" {
		return
	}
	validateK8sName("entrypoint name", name)
}

func validateK8sName(label, name string) {
	if len(name) > 63 {
		panic(fmt.Sprintf("%s %q exceeds 63 characters", label, name))
	}

	for i, char := range name {
		if !((char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-') {
			panic(fmt.Sprintf("%s %q contains invalid character %q at position %d; must contain only lowercase letters, digits, and hyphens", label, name, string(char), i))
		}
	}

	if name[0] == '-' || name[len(name)-1] == '-' {
		panic(fmt.Sprintf("%s %q must not start or end with a hyphen", label, name))
	}
}

func (p *Plugin) resolveEntrypoint(entrypoint string) string {
	if entrypoint == "" {
		return p.name
	}
	return entrypoint
}
