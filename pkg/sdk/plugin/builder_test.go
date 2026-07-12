package plugin

import (
	"context"
	"strings"
	"testing"
	"time"

	v1 "kubevirt.io/api/core/v1"
	"libvirt.org/go/libvirtxml"
)

type stubDomainHandler struct{}

func (s *stubDomainHandler) MutateDomain(_ context.Context, _ *libvirtxml.Domain, _ *v1.VirtualMachineInstance) error {
	return nil
}

type stubNodeHandler struct{}

func (s *stubNodeHandler) ExecuteNodeHook(_ context.Context, _ *NodeHookRequest) error {
	return nil
}

func TestNewCreatesPluginWithName(t *testing.T) {
	p := New("my-plugin")

	if p.name != "my-plugin" {
		t.Fatalf("expected name 'my-plugin', got %q", p.name)
	}
}

func TestWithDomainHookSetsHandler(t *testing.T) {
	handler := &stubDomainHandler{}

	p := New("test").WithDomainHook(ForLibvirt(handler))
	if len(p.domainHooks) == 0 {
		t.Fatal("expected domainHooks to be set")
	}

	if p.domainHooks[0].handler != handler {
		t.Fatal("expected handler to match")
	}
}

func TestWithNodeHookAddsHookPoint(t *testing.T) {
	handler := &stubNodeHandler{}

	p := New("test").WithNodeHook(PreVMStart, NodeHandler(handler))
	if len(p.nodeHooks) != 1 {
		t.Fatalf("expected 1 node hook, got %d", len(p.nodeHooks))
	}

	if p.nodeHooks[0].hookPoint != PreVMStart {
		t.Fatalf("expected hook point %q, got %q", PreVMStart, p.nodeHooks[0].hookPoint)
	}

	if p.nodeHooks[0].handler != handler {
		t.Fatal("expected handler to match")
	}
}

func TestWithMultipleNodeHooks(t *testing.T) {
	handler := &stubNodeHandler{}

	p := New("test").
		WithNodeHook(PreVMStart, NodeHandler(handler)).
		WithNodeHook(PostVMStop, NodeHandler(handler)).
		WithNodeHook(PreMigrationSource, NodeHandler(handler))
	if len(p.nodeHooks) != 3 {
		t.Fatalf("expected 3 node hooks, got %d", len(p.nodeHooks))
	}

	expected := []string{PreVMStart, PostVMStop, PreMigrationSource}
	for i, hp := range expected {
		if p.nodeHooks[i].hookPoint != hp {
			t.Fatalf("node hook %d: expected %q, got %q", i, hp, p.nodeHooks[i].hookPoint)
		}
	}
}

func TestWithConditionSetsPluginLevelCondition(t *testing.T) {
	p := New("test").WithCondition("vmi.labels.gpu == 'true'")

	if p.condition != "vmi.labels.gpu == 'true'" {
		t.Fatalf("expected condition to be set, got %q", p.condition)
	}
}

func TestWithFailureStrategySetsPluginLevelDefault(t *testing.T) {
	p := New("test").WithFailureStrategy(Ignore)

	if p.failureStrategy != Ignore {
		t.Fatalf("expected failure strategy %q, got %q", Ignore, p.failureStrategy)
	}
}

func TestDomainHookPerHookSettings(t *testing.T) {
	handler := &stubDomainHandler{}
	timeout := 30 * time.Second

	p := New("test").WithDomainHook(
		ForLibvirt(handler).
			WithCondition("vmi.spec.domain.devices.gpus != null").
			WithFailureStrategy(Ignore).
			WithTimeout(timeout),
	)

	domainHook := p.domainHooks[0]

	if domainHook.condition != "vmi.spec.domain.devices.gpus != null" {
		t.Fatalf("expected condition, got %q", domainHook.condition)
	}

	if domainHook.failureStrategy == nil || *domainHook.failureStrategy != Ignore {
		t.Fatal("expected failure strategy Ignore")
	}

	if domainHook.timeout == nil || *domainHook.timeout != timeout {
		t.Fatalf("expected timeout %v, got %v", timeout, domainHook.timeout)
	}
}

func TestNodeHookPerHookSettings(t *testing.T) {
	handler := &stubNodeHandler{}
	timeout := 60 * time.Second

	p := New("test").WithNodeHook(PreVMStart,
		NodeHandler(handler).
			WithCondition("vmi.metadata.name == 'test'").
			WithFailureStrategy(Fail).
			WithTimeout(timeout),
	)
	if len(p.nodeHooks) != 1 {
		t.Fatalf("expected 1 node hook, got %d", len(p.nodeHooks))
	}

	nodeHook := p.nodeHooks[0]

	if nodeHook.condition != "vmi.metadata.name == 'test'" {
		t.Fatalf("expected condition, got %q", nodeHook.condition)
	}

	if nodeHook.failureStrategy == nil || *nodeHook.failureStrategy != Fail {
		t.Fatal("expected failure strategy Fail")
	}

	if nodeHook.timeout == nil || *nodeHook.timeout != timeout {
		t.Fatalf("expected timeout %v, got %v", timeout, nodeHook.timeout)
	}
}

func TestFluentChaining(t *testing.T) {
	domainHandler := &stubDomainHandler{}
	nodeHandler := &stubNodeHandler{}

	p := New("full-plugin").
		WithDomainHook(ForLibvirt(domainHandler)).
		WithNodeHook(PreVMStart, NodeHandler(nodeHandler)).
		WithNodeHook(PostVMStop, NodeHandler(nodeHandler)).
		WithCondition("vmi.labels.special == 'true'").
		WithFailureStrategy(Ignore)

	if p.name != "full-plugin" {
		t.Fatalf("expected name 'full-plugin', got %q", p.name)
	}

	if len(p.domainHooks) == 0 {
		t.Fatal("expected domain hooks to be set")
	}

	if len(p.nodeHooks) != 2 {
		t.Fatalf("expected 2 node hooks, got %d", len(p.nodeHooks))
	}

	if p.condition != "vmi.labels.special == 'true'" {
		t.Fatalf("expected condition, got %q", p.condition)
	}

	if p.failureStrategy != Ignore {
		t.Fatalf("expected Ignore, got %q", p.failureStrategy)
	}
}

func TestMultipleDomainHooks(t *testing.T) {
	handler1 := &stubDomainHandler{}
	handler2 := &stubDomainHandler{}

	p := New("test").
		WithDomainHook(ForLibvirt(handler1)).
		WithDomainHook(ForLibvirt(handler2))

	if len(p.domainHooks) != 2 {
		t.Fatalf("expected 2 domain hooks, got %d", len(p.domainHooks))
	}

	if p.domainHooks[0].handler != handler1 {
		t.Fatal("expected first handler to match")
	}

	if p.domainHooks[1].handler != handler2 {
		t.Fatal("expected second handler to match")
	}
}

func TestWithEntrypointDomainHook(t *testing.T) {
	handler := &stubDomainHandler{}

	opt := ForLibvirt(handler).WithEntrypoint("my-sidecar")

	if opt.entrypoint != "my-sidecar" {
		t.Fatalf("expected entrypoint 'my-sidecar', got %q", opt.entrypoint)
	}
}

func TestWithEntrypointNodeHook(t *testing.T) {
	handler := &stubNodeHandler{}

	opt := NodeHandler(handler).WithEntrypoint("my-daemon")

	if opt.entrypoint != "my-daemon" {
		t.Fatalf("expected entrypoint 'my-daemon', got %q", opt.entrypoint)
	}
}

func TestDefaultEntrypointIsEmpty(t *testing.T) {
	domainOpt := ForLibvirt(&stubDomainHandler{})
	if domainOpt.entrypoint != "" {
		t.Fatalf("expected empty domain hook entrypoint, got %q", domainOpt.entrypoint)
	}

	nodeOpt := NodeHandler(&stubNodeHandler{})
	if nodeOpt.entrypoint != "" {
		t.Fatalf("expected empty node hook entrypoint, got %q", nodeOpt.entrypoint)
	}
}

func TestDuplicateHookPointDifferentEntrypoints(t *testing.T) {
	handler := &stubNodeHandler{}

	p := New("test").
		WithNodeHook(PreVMStart, NodeHandler(handler).WithEntrypoint("ep-a")).
		WithNodeHook(PreVMStart, NodeHandler(handler).WithEntrypoint("ep-b"))

	if len(p.nodeHooks) != 2 {
		t.Fatalf("expected 2 node hooks, got %d", len(p.nodeHooks))
	}
}

func TestDuplicateHookPointSameEntrypointPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate hook point within same entrypoint")
		}
	}()

	handler := &stubNodeHandler{}
	New("test").
		WithNodeHook(PreVMStart, NodeHandler(handler).WithEntrypoint("ep-a")).
		WithNodeHook(PreVMStart, NodeHandler(handler).WithEntrypoint("ep-a"))
}

func TestPanicsOnInvalidPluginName(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"uppercase", "MyPlugin"},
		{"underscore", "my_plugin"},
		{"too long", strings.Repeat("a", 64)},
		{"leading hyphen", "-my-plugin"},
		{"trailing hyphen", "my-plugin-"},
		{"dots", "my.plugin"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("expected panic for plugin name %q", tc.input)
				}
			}()
			New(tc.input)
		})
	}
}

func TestPanicsOnEmptyName(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on empty name")
		}
	}()

	New("")
}

func TestPanicsOnInvalidHookPoint(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on invalid hook point")
		}
	}()

	handler := &stubNodeHandler{}
	New("test").WithNodeHook("InvalidHookPoint", NodeHandler(handler))
}

func TestPanicsOnDuplicateHookPoint(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate hook point")
		}
	}()

	handler := &stubNodeHandler{}
	New("test").
		WithNodeHook(PreVMStart, NodeHandler(handler)).
		WithNodeHook(PreVMStart, NodeHandler(handler))
}

func TestWithNamespaceSetsNamespace(t *testing.T) {
	p := New("test").WithNamespace("my-namespace")

	if p.namespace != "my-namespace" {
		t.Fatalf("expected namespace 'my-namespace', got %q", p.namespace)
	}
}

func TestDefaultNamespace(t *testing.T) {
	p := New("test")

	if p.namespace != "default" {
		t.Fatalf("expected default namespace 'default', got %q", p.namespace)
	}
}

func TestPanicsOnEmptyNamespace(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on empty namespace")
		}
	}()

	New("test").WithNamespace("")
}

func TestEntrypointValidationPanics(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"uppercase", "MyEntrypoint"},
		{"dots", "my.entrypoint"},
		{"slash", "my/entrypoint"},
		{"leading hyphen", "-my-ep"},
		{"trailing hyphen", "my-ep-"},
		{"spaces", "my ep"},
		{"too long", strings.Repeat("a", 64)},
		{"underscore", "my_ep"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("expected panic for entrypoint %q", tc.input)
				}
			}()

			ForLibvirt(&stubDomainHandler{}).WithEntrypoint(tc.input)
		})
	}
}

func TestEntrypointValidationNodeHookPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for invalid node hook entrypoint")
		}
	}()

	NodeHandler(&stubNodeHandler{}).WithEntrypoint("INVALID")
}

func TestEntrypointValidationAcceptsValid(t *testing.T) {
	validNames := []string{
		"my-ep",
		"a",
		"my-ep-123",
		"123",
		strings.Repeat("a", 63),
	}

	for _, name := range validNames {
		opt := ForLibvirt(&stubDomainHandler{}).WithEntrypoint(name)

		if opt.entrypoint != name {
			t.Fatalf("expected entrypoint %q, got %q", name, opt.entrypoint)
		}
	}
}

func TestDuplicateHookPointResolvedEntrypointPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when empty and explicit entrypoints resolve to same value")
		}
	}()

	handler := &stubNodeHandler{}
	New("my-plugin").
		WithNodeHook(PreVMStart, NodeHandler(handler)).
		WithNodeHook(PreVMStart, NodeHandler(handler).WithEntrypoint("my-plugin"))
}

func TestWithNodeHookDoesNotMutateCallerOption(t *testing.T) {
	handler := &stubNodeHandler{}
	opt := NodeHandler(handler)

	if opt.hookPoint != "" {
		t.Fatal("expected empty hookPoint before WithNodeHook")
	}

	New("test").WithNodeHook(PreVMStart, opt)

	if opt.hookPoint != "" {
		t.Fatalf("expected hookPoint to remain empty after WithNodeHook, got %q", opt.hookPoint)
	}
}

func TestWithInvalidCELConditionPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on invalid CEL condition for domain hook")
		}
	}()

	ForLibvirt(&stubDomainHandler{}).WithCondition("invalid >>><< expression")
}

func TestWithInvalidCELConditionOnNodeHookPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on invalid CEL condition for node hook")
		}
	}()

	NodeHandler(&stubNodeHandler{}).WithCondition("invalid >>><< expression")
}

func TestWithInvalidPluginLevelCELConditionPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on invalid plugin-level CEL condition")
		}
	}()

	New("test").WithCondition("invalid >>><< expression")
}

func TestWithValidCELConditionSucceeds(t *testing.T) {
	domainOpt := ForLibvirt(&stubDomainHandler{}).WithCondition("vmi.spec.domain.cpu.cores > 1")
	if domainOpt.condition != "vmi.spec.domain.cpu.cores > 1" {
		t.Fatalf("expected domain hook condition to be set, got %q", domainOpt.condition)
	}

	nodeOpt := NodeHandler(&stubNodeHandler{}).WithCondition("vmi.metadata.name == 'test'")
	if nodeOpt.condition != "vmi.metadata.name == 'test'" {
		t.Fatalf("expected node hook condition to be set, got %q", nodeOpt.condition)
	}

	p := New("test").WithCondition("vmi.spec.domain.cpu.cores > 1")
	if p.condition != "vmi.spec.domain.cpu.cores > 1" {
		t.Fatalf("expected plugin condition to be set, got %q", p.condition)
	}
}

func TestCELDomainHookCreation(t *testing.T) {
	opt := CELDomainHook("domain.name == 'test'")

	if !opt.isCEL() {
		t.Fatal("expected isCEL to be true")
	}

	if opt.expression != "domain.name == 'test'" {
		t.Fatalf("expected expression, got %q", opt.expression)
	}
}

func TestCELDomainHookEmptyExpressionPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on empty CEL expression")
		}
	}()

	CELDomainHook("")
}

func TestCELDomainHookInvalidExpressionPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on invalid CEL expression")
		}
	}()

	CELDomainHook("invalid >>><< expression")
}

func TestCELDomainHookWithPerHookSettings(t *testing.T) {
	timeout := 30 * time.Second
	opt := CELDomainHook("domain.name == 'test'").
		WithCondition("vmi.metadata.name == 'my-vmi'").
		WithFailureStrategy(Ignore).
		WithTimeout(timeout)

	if opt.condition != "vmi.metadata.name == 'my-vmi'" {
		t.Fatalf("expected condition, got %q", opt.condition)
	}

	if opt.failureStrategy == nil || *opt.failureStrategy != Ignore {
		t.Fatal("expected failure strategy Ignore")
	}

	if opt.timeout == nil || *opt.timeout != timeout {
		t.Fatal("expected timeout 30s")
	}
}

func TestCELDomainHookWithEntrypointPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on CEL hook with entrypoint")
		}
	}()

	CELDomainHook("domain.name == 'test'").WithEntrypoint("my-ep")
}

func TestWithDomainCELHookConvenience(t *testing.T) {
	p := New("test").WithDomainCELHook("domain.name == 'test'")

	if len(p.domainHooks) != 1 {
		t.Fatalf("expected 1 domain hook, got %d", len(p.domainHooks))
	}

	if !p.domainHooks[0].isCEL() {
		t.Fatal("expected CEL domain hook")
	}
}

func TestMixedSidecarAndCELDomainHooks(t *testing.T) {
	handler := &stubDomainHandler{}
	p := New("test").
		WithDomainHook(ForLibvirt(handler)).
		WithDomainCELHook("domain.name == 'test'").
		WithDomainHook(ForLibvirt(handler))

	if len(p.domainHooks) != 3 {
		t.Fatalf("expected 3 domain hooks, got %d", len(p.domainHooks))
	}

	if p.domainHooks[0].isCEL() {
		t.Fatal("expected first hook to be sidecar")
	}

	if !p.domainHooks[1].isCEL() {
		t.Fatal("expected second hook to be CEL")
	}

	if p.domainHooks[2].isCEL() {
		t.Fatal("expected third hook to be sidecar")
	}
}
