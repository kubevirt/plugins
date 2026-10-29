package plugin

import (
	"fmt"
	"slices"
	"time"

	"github.com/kubevirt/plugins/pkg/sdk/cel"
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
	image           string
	imagePullPolicy string
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
	hookPoint       NodeHookPoint
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

func (p *Plugin) WithNodeHook(hookPoint NodeHookPoint, opt *NodeHookOption) *Plugin {
	if opt == nil || opt.handler == nil {
		panic("node hook must have a handler")
	}
	if !slices.Contains(AllNodeHookPoints(), hookPoint) {
		panic(fmt.Sprintf("invalid hook point %q; valid hook points: %v", hookPoint, AllNodeHookPoints()))
	}
	resolvedEntrypoint := p.resolveEntrypoint(opt.entrypoint)
	validateNodeSocketPath(NodeSocketPathForEntrypoint(p.name, resolvedEntrypoint))
	validateK8sName("daemonset name", p.entrypointResourceName(resolvedEntrypoint))
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
	if err := cel.ValidateCondition(condition); err != nil {
		panic(fmt.Sprintf("invalid plugin-level CEL condition: %v", err))
	}
	p.condition = condition
	return p
}

func (p *Plugin) WithFailureStrategy(strategy FailureStrategy) *Plugin {
	validateFailureStrategy(strategy)
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

func (p *Plugin) WithImage(image string) *Plugin {
	if image == "" {
		panic("image must not be empty")
	}
	p.image = image
	return p
}

func (p *Plugin) WithImagePullPolicy(policy string) *Plugin {
	if policy == "" {
		panic("image pull policy must not be empty")
	}
	p.imagePullPolicy = policy
	return p
}

func ForLibvirt(handler LibvirtDomainHookHandler) *DomainHookOption {
	return &DomainHookOption{handler: handler}
}

func CELDomainHook(expression string) *DomainHookOption {
	if expression == "" {
		panic("CEL domain hook expression must not be empty")
	}
	if err := cel.ValidateGuestDefinitionExpression(expression); err != nil {
		panic(fmt.Sprintf("invalid CEL domain hook expression: %v", err))
	}
	return &DomainHookOption{expression: expression}
}

func (p *Plugin) WithDomainCELHook(expression string) *Plugin {
	return p.WithDomainHook(CELDomainHook(expression))
}

func (o *DomainHookOption) WithCondition(condition string) *DomainHookOption {
	if err := cel.ValidateCondition(condition); err != nil {
		panic(fmt.Sprintf("invalid domain hook CEL condition: %v", err))
	}
	o.condition = condition
	return o
}

func (o *DomainHookOption) WithFailureStrategy(strategy FailureStrategy) *DomainHookOption {
	validateFailureStrategy(strategy)
	o.failureStrategy = &strategy
	return o
}

func (o *DomainHookOption) WithTimeout(timeout time.Duration) *DomainHookOption {
	validateTimeout(timeout)
	o.timeout = &timeout
	return o
}

func NodeHandler(handler NodeHookHandler) *NodeHookOption {
	if handler == nil {
		panic("node hook must have a handler")
	}
	return &NodeHookOption{handler: handler}
}

func (o *NodeHookOption) WithCondition(condition string) *NodeHookOption {
	if err := cel.ValidateCondition(condition); err != nil {
		panic(fmt.Sprintf("invalid node hook CEL condition: %v", err))
	}
	o.condition = condition
	return o
}

func (o *NodeHookOption) WithFailureStrategy(strategy FailureStrategy) *NodeHookOption {
	validateFailureStrategy(strategy)
	o.failureStrategy = &strategy
	return o
}

func (o *NodeHookOption) WithTimeout(timeout time.Duration) *NodeHookOption {
	validateTimeout(timeout)
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

func validateFailureStrategy(strategy FailureStrategy) {
	if strategy != Fail && strategy != Ignore {
		panic(fmt.Sprintf("invalid failure strategy %q; must be %q or %q", strategy, Fail, Ignore))
	}
}

func validateTimeout(timeout time.Duration) {
	if timeout <= 0 {
		panic(fmt.Sprintf("timeout must be greater than zero, got %v", timeout))
	}
}

// validateNodeSocketPath rejects paths Go cannot listen on. A non-abstract
// unix socket path must leave room for the null terminator in sun_path's
// 108 bytes, so 107 is the longest usable length; a path of exactly 108
// bytes, which KubeVirt's CRD still accepts, already fails at listen time.
func validateNodeSocketPath(path string) {
	if len(path) > 107 {
		panic(fmt.Sprintf("node socket path %q exceeds 107 bytes", path))
	}
}

func (p *Plugin) resolveEntrypoint(entrypoint string) string {
	if entrypoint == "" {
		return p.name
	}
	return entrypoint
}

// entrypointResourceName returns the name of the per-entrypoint DaemonSet.
func (p *Plugin) entrypointResourceName(entrypoint string) string {
	if entrypoint == p.name {
		return p.name
	}
	return p.name + "-" + entrypoint
}

func (p *Plugin) resolveImage() string {
	if p.image != "" {
		return p.image
	}
	return "quay.io/myorg/" + p.name + ":latest"
}
