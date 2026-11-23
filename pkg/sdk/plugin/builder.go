package plugin

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/kubevirt/plugins/pkg/sdk/cel"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
)

// maxHooksPerPlugin matches the Plugin CRD's MaxItems bound on
// launcherHooks and nodeHooks.
const maxHooksPerPlugin = 64

// Plugin is a KubeVirt plugin under construction. Configure it fully,
// including registering every hook, before calling Serve or Generate; the
// fields are not synchronized for concurrent use.
type Plugin struct {
	name             string
	launcherHooks    []LauncherHookOption
	nodeHooks        []NodeHookOption
	condition        string
	failureStrategy  FailureStrategy
	namespace        string
	rbacRules        []rbacv1.PolicyRule
	image            string
	imagePullPolicy  string
	sidecarPodLabels map[string]string
	sidecarResources corev1.ResourceRequirements
}

// LauncherHookOption configures one launcher hook; a constructor picks the
// hook point and the With* methods refine it.
type LauncherHookOption struct {
	hookPoint                 LauncherHookPoint
	guestDefinitionHandler    GuestDefinitionHandler
	preBootHandler            PreBootHandler
	preMigrationSourceHandler PreMigrationSourceHandler
	expression                string
	condition                 string
	failureStrategy           *FailureStrategy
	timeout                   *time.Duration
	entrypoint                string
}

func (o *LauncherHookOption) isCEL() bool {
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
	validateK8sName("plugin name", name)
	return &Plugin{name: name, namespace: "default"}
}

// GuestDefinition returns a sidecar hook that mutates the domain XML through handler.
func GuestDefinition(handler GuestDefinitionHandler) *LauncherHookOption {
	if handler == nil {
		panic("GuestDefinition handler must not be nil")
	}
	return &LauncherHookOption{hookPoint: LauncherHookGuestDefinition, guestDefinitionHandler: handler}
}

// GuestDefinitionCEL returns a CEL hook that KubeVirt evaluates directly; expression must return a sparse Domain.
func GuestDefinitionCEL(expression string) *LauncherHookOption {
	if expression == "" {
		panic("CEL GuestDefinition expression must not be empty")
	}
	if err := cel.ValidateGuestDefinitionExpression(expression); err != nil {
		panic(fmt.Sprintf("invalid CEL GuestDefinition expression: %v", err))
	}
	return &LauncherHookOption{hookPoint: LauncherHookGuestDefinition, expression: expression}
}

// PreBoot returns a sidecar hook that runs before the guest starts.
func PreBoot(handler PreBootHandler) *LauncherHookOption {
	if handler == nil {
		panic("PreBoot handler must not be nil")
	}
	return &LauncherHookOption{hookPoint: LauncherHookPreBoot, preBootHandler: handler}
}

// PreMigrationSource returns a sidecar hook that adjusts a migration on its source.
func PreMigrationSource(handler PreMigrationSourceHandler) *LauncherHookOption {
	if handler == nil {
		panic("PreMigrationSource handler must not be nil")
	}
	return &LauncherHookOption{hookPoint: LauncherHookPreMigrationSource, preMigrationSourceHandler: handler}
}

func (p *Plugin) WithLauncherHook(opt *LauncherHookOption) *Plugin {
	if opt == nil {
		panic("launcher hook must not be nil")
	}
	if opt.hookPoint == "" {
		panic("launcher hook must have a hook point")
	}
	if !opt.isCEL() && opt.guestDefinitionHandler == nil && opt.preBootHandler == nil && opt.preMigrationSourceHandler == nil {
		panic("launcher hook must have a handler or a CEL expression")
	}
	if len(p.launcherHooks) >= maxHooksPerPlugin {
		panic(fmt.Sprintf("plugin already has %d launcher hooks; the Plugin CRD allows at most %d", len(p.launcherHooks), maxHooksPerPlugin))
	}

	hookCopy := *opt
	if hookCopy.isCEL() {
		p.launcherHooks = append(p.launcherHooks, hookCopy)
		return p
	}

	resolvedEntrypoint := p.resolveEntrypoint(hookCopy.entrypoint)
	validateSocketPath("launcher socket path", LauncherSocketPath(p.name, resolvedEntrypoint))
	validateK8sName("sidecar container name", p.sidecarContainerName(resolvedEntrypoint))
	for _, existingHook := range p.launcherHooks {
		if existingHook.isCEL() {
			continue
		}
		if existingHook.hookPoint == hookCopy.hookPoint && p.resolveEntrypoint(existingHook.entrypoint) == resolvedEntrypoint {
			panic(fmt.Sprintf("duplicate hook point %q for entrypoint %q; each hook point can only be registered once per entrypoint", hookCopy.hookPoint, resolvedEntrypoint))
		}
	}

	p.launcherHooks = append(p.launcherHooks, hookCopy)
	return p
}

func (p *Plugin) WithNodeHook(hookPoint NodeHookPoint, opt *NodeHookOption) *Plugin {
	if opt == nil || opt.handler == nil {
		panic("node hook must have a handler")
	}
	if !slices.Contains(AllNodeHookPoints(), hookPoint) {
		panic(fmt.Sprintf("invalid hook point %q; valid hook points: %v", hookPoint, AllNodeHookPoints()))
	}
	if len(p.nodeHooks) >= maxHooksPerPlugin {
		panic(fmt.Sprintf("plugin already has %d node hooks; the Plugin CRD allows at most %d", len(p.nodeHooks), maxHooksPerPlugin))
	}
	resolvedEntrypoint := p.resolveEntrypoint(opt.entrypoint)
	validateSocketPath("node socket path", NodeSocketPath(p.name, resolvedEntrypoint))
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

// WithFailureStrategy sets the plugin-level failure strategy, the default
// for hooks that do not set their own.
func (p *Plugin) WithFailureStrategy(strategy FailureStrategy) *Plugin {
	validateFailureStrategy(strategy)
	p.failureStrategy = strategy
	return p
}

func (p *Plugin) WithNamespace(namespace string) *Plugin {
	if namespace == "" {
		panic("namespace must not be empty")
	}
	validateK8sName("namespace", namespace)
	p.namespace = namespace
	return p
}

func (p *Plugin) WithImage(image string) *Plugin {
	if image == "" {
		panic("image must not be empty")
	}
	if strings.Contains(image, "'") {
		panic("image must not contain a single quote; the generated manifests embed it in a single-quoted YAML scalar")
	}
	p.image = image
	return p
}

func (p *Plugin) WithImagePullPolicy(policy string) *Plugin {
	switch corev1.PullPolicy(policy) {
	case corev1.PullAlways, corev1.PullIfNotPresent, corev1.PullNever:
	default:
		panic(fmt.Sprintf("invalid image pull policy %q; must be %q, %q or %q", policy, corev1.PullAlways, corev1.PullIfNotPresent, corev1.PullNever))
	}
	p.imagePullPolicy = policy
	return p
}

// WithSidecarPodLabels restricts the generated sidecar injection to
// virt-launcher pods carrying these labels. KubeVirt copies VMI labels to
// the launcher pod, so labelling VMIs scopes the plugin to them; without it
// the sidecar is injected into every virt-launcher pod while the Plugins
// feature gate is on - see the README section on sidecar injection scope.
// Plugins without sidecar hooks generate no injection policy, so the labels
// are ignored for them.
func (p *Plugin) WithSidecarPodLabels(labels map[string]string) *Plugin {
	if len(labels) == 0 {
		panic("sidecar pod labels must not be empty")
	}
	for key, value := range labels {
		validateSidecarPodLabel(key, value)
	}
	p.sidecarPodLabels = labels
	return p
}

// WithSidecarResources sets the resource requests and limits rendered into
// every injected sidecar container. An injection without requests makes the
// virt-launcher pod Burstable, which breaks Guaranteed pod QoS and
// dedicated-CPU VMIs, so set them to keep those working.
func (p *Plugin) WithSidecarResources(resources corev1.ResourceRequirements) *Plugin {
	p.sidecarResources = resources
	return p
}

// WithRBACRules sets the RBAC rules rendered into the service account of the
// node-hook DaemonSet; sidecar-only plugins get no RBAC bundle, so the rules
// are ignored for them.
func (p *Plugin) WithRBACRules(rules []rbacv1.PolicyRule) *Plugin {
	p.rbacRules = rules
	return p
}

func NodeHandler(handler NodeHookHandler) *NodeHookOption {
	if handler == nil {
		panic("node hook must have a handler")
	}
	return &NodeHookOption{handler: handler}
}

func (o *LauncherHookOption) WithCondition(condition string) *LauncherHookOption {
	if err := cel.ValidateCondition(condition); err != nil {
		panic(fmt.Sprintf("invalid launcher hook CEL condition: %v", err))
	}
	o.condition = condition
	return o
}

func (o *LauncherHookOption) WithFailureStrategy(strategy FailureStrategy) *LauncherHookOption {
	validateFailureStrategy(strategy)
	o.failureStrategy = &strategy
	return o
}

func (o *LauncherHookOption) WithTimeout(timeout time.Duration) *LauncherHookOption {
	if o.isCEL() {
		panic("CEL launcher hooks do not support timeouts; KubeVirt ignores them")
	}
	validateTimeout(timeout)
	o.timeout = &timeout
	return o
}

func (o *LauncherHookOption) WithEntrypoint(name string) *LauncherHookOption {
	if o.isCEL() {
		panic("CEL launcher hooks do not support entrypoints; KubeVirt evaluates them without a container")
	}
	validateEntrypoint(name)
	o.entrypoint = name
	return o
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

func (o *NodeHookOption) WithEntrypoint(name string) *NodeHookOption {
	validateEntrypoint(name)
	o.entrypoint = name
	return o
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

// validateSidecarPodLabel checks that key and value form a valid Kubernetes
// label; both are embedded in the generated CEL match conditions, whose
// string literals the label character set keeps safe.
func validateSidecarPodLabel(key, value string) {
	if key == "" {
		panic("sidecar pod label key must not be empty")
	}

	name := key
	if separator := strings.LastIndex(key, "/"); separator >= 0 {
		prefix := key[:separator]
		name = key[separator+1:]
		if len(prefix) > 253 {
			panic(fmt.Sprintf("sidecar pod label key %q prefix exceeds 253 characters", key))
		}
		for i, char := range prefix {
			if !((char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-' || char == '.') {
				panic(fmt.Sprintf("sidecar pod label key %q prefix contains invalid character %q at position %d; must contain only lowercase letters, digits, '-' and '.'", key, string(char), i))
			}
		}
	}

	validateK8sLabelPart(fmt.Sprintf("sidecar pod label key %q", key), name)
	validateK8sLabelPart(fmt.Sprintf("sidecar pod label value for key %q", key), value)
}

// validateK8sLabelPart validates s as the name or value part of a Kubernetes
// label: at most 63 characters of letters, digits, '-', '_' and '.',
// starting and ending with a letter or digit. Values may be empty.
func validateK8sLabelPart(label, s string) {
	if len(s) > 63 {
		panic(fmt.Sprintf("%s %q exceeds 63 characters", label, s))
	}

	if s == "" {
		return
	}

	for i, char := range s {
		if !isLetterOrDigit(byte(char)) && char != '-' && char != '_' && char != '.' {
			panic(fmt.Sprintf("%s %q contains invalid character %q at position %d; must contain only letters, digits, '-', '_' and '.'", label, s, string(char), i))
		}
	}

	if !isLetterOrDigit(s[0]) || !isLetterOrDigit(s[len(s)-1]) {
		panic(fmt.Sprintf("%s %q must start and end with a letter or digit", label, s))
	}
}

func isLetterOrDigit(char byte) bool {
	return (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9')
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

// validateSocketPath rejects paths Go cannot listen on. A non-abstract
// unix socket path must leave room for the null terminator in sun_path's
// 108 bytes, so 107 is the longest usable length; a path of exactly 108
// bytes, which KubeVirt's CRD still accepts, already fails at listen time.
func validateSocketPath(label, path string) {
	if len(path) > 107 {
		panic(fmt.Sprintf("%s %q exceeds 107 bytes", label, path))
	}
}

func (p *Plugin) resolveEntrypoint(entrypoint string) string {
	return cmp.Or(entrypoint, p.name)
}

// entrypointResourceName returns the name of the per-entrypoint DaemonSet
// and MutatingAdmissionPolicy objects.
func (p *Plugin) entrypointResourceName(entrypoint string) string {
	if entrypoint == p.name {
		return p.name
	}
	return p.name + "-" + entrypoint
}

func (p *Plugin) sidecarContainerName(entrypoint string) string {
	if entrypoint == p.name {
		return "plugin-sidecar-" + p.name
	}
	return "plugin-sidecar-" + p.name + "-" + entrypoint
}

func (p *Plugin) resolveImage() string {
	return cmp.Or(p.image, "quay.io/myorg/"+p.name+":latest")
}
