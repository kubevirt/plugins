package plugin

import (
	"context"
	"fmt"
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

func (s *stubNodeHandler) ExecuteNodeHook(_ context.Context, _ *NodeHookRequest) (*NodeHookResponse, error) {
	return nil, nil
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

	p := New("test").WithNodeHook(NodeHookPreVMStart, NodeHandler(handler))
	if len(p.nodeHooks) != 1 {
		t.Fatalf("expected 1 node hook, got %d", len(p.nodeHooks))
	}

	if p.nodeHooks[0].hookPoint != NodeHookPreVMStart {
		t.Fatalf("expected hook point %q, got %q", NodeHookPreVMStart, p.nodeHooks[0].hookPoint)
	}

	if p.nodeHooks[0].handler != handler {
		t.Fatal("expected handler to match")
	}
}

func TestWithMultipleNodeHooks(t *testing.T) {
	handler := &stubNodeHandler{}

	p := New("test").
		WithNodeHook(NodeHookPreVMStart, NodeHandler(handler)).
		WithNodeHook(NodeHookPostVMStop, NodeHandler(handler)).
		WithNodeHook(NodeHookPreMigrationSource, NodeHandler(handler))
	if len(p.nodeHooks) != 3 {
		t.Fatalf("expected 3 node hooks, got %d", len(p.nodeHooks))
	}

	expected := []NodeHookPoint{NodeHookPreVMStart, NodeHookPostVMStop, NodeHookPreMigrationSource}
	for i, hp := range expected {
		if p.nodeHooks[i].hookPoint != hp {
			t.Fatalf("node hook %d: expected %q, got %q", i, hp, p.nodeHooks[i].hookPoint)
		}
	}
}

func TestWithConditionSetsPluginLevelCondition(t *testing.T) {
	p := New("test").WithCondition("vmi.Name == 'test'")

	if p.condition != "vmi.Name == 'test'" {
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
			WithCondition("vmi.Spec.Domain.CPU.Cores > 2").
			WithFailureStrategy(Ignore).
			WithTimeout(timeout),
	)

	domainHook := p.domainHooks[0]

	if domainHook.condition != "vmi.Spec.Domain.CPU.Cores > 2" {
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

	p := New("test").WithNodeHook(NodeHookPreVMStart,
		NodeHandler(handler).
			WithCondition("vmi.Name == 'test'").
			WithFailureStrategy(Fail).
			WithTimeout(timeout),
	)
	if len(p.nodeHooks) != 1 {
		t.Fatalf("expected 1 node hook, got %d", len(p.nodeHooks))
	}

	nodeHook := p.nodeHooks[0]

	if nodeHook.condition != "vmi.Name == 'test'" {
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
		WithNodeHook(NodeHookPreVMStart, NodeHandler(nodeHandler)).
		WithNodeHook(NodeHookPostVMStop, NodeHandler(nodeHandler)).
		WithCondition("vmi.Name == 'test'").
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

	if p.condition != "vmi.Name == 'test'" {
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
		WithNodeHook(NodeHookPreVMStart, NodeHandler(handler).WithEntrypoint("ep-a")).
		WithNodeHook(NodeHookPreVMStart, NodeHandler(handler).WithEntrypoint("ep-b"))

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
		WithNodeHook(NodeHookPreVMStart, NodeHandler(handler).WithEntrypoint("ep-a")).
		WithNodeHook(NodeHookPreVMStart, NodeHandler(handler).WithEntrypoint("ep-a"))
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

func TestPanicsOnNilNodeHandler(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on nil NodeHandler handler")
		}
	}()

	NodeHandler(nil)
}

func TestPanicsOnNodeHookWithoutHandler(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on node hook without a handler")
		}
	}()

	New("test").WithNodeHook(NodeHookPreVMStart, &NodeHookOption{})
}

func TestPanicsOnInvalidHookPoint(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on invalid hook point")
		}
	}()

	handler := &stubNodeHandler{}
	New("test").WithNodeHook(NodeHookPoint("InvalidHookPoint"), NodeHandler(handler))
}

func TestPanicsOnInvalidFailureStrategy(t *testing.T) {
	invalid := FailureStrategy("BestEffort")

	t.Run("plugin level", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic on invalid plugin-level failure strategy")
			}
		}()
		New("test").WithFailureStrategy(invalid)
	})

	t.Run("domain hook", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic on invalid domain hook failure strategy")
			}
		}()
		ForLibvirt(&stubDomainHandler{}).WithFailureStrategy(invalid)
	})

	t.Run("node hook", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic on invalid node hook failure strategy")
			}
		}()
		NodeHandler(&stubNodeHandler{}).WithFailureStrategy(invalid)
	})
}

func TestPanicsOnNonPositiveTimeout(t *testing.T) {
	t.Run("domain hook zero", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic on zero domain hook timeout")
			}
		}()
		ForLibvirt(&stubDomainHandler{}).WithTimeout(0)
	})

	t.Run("domain hook negative", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic on negative domain hook timeout")
			}
		}()
		ForLibvirt(&stubDomainHandler{}).WithTimeout(-time.Second)
	})

	t.Run("node hook zero", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic on zero node hook timeout")
			}
		}()
		NodeHandler(&stubNodeHandler{}).WithTimeout(0)
	})

	t.Run("node hook negative", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic on negative node hook timeout")
			}
		}()
		NodeHandler(&stubNodeHandler{}).WithTimeout(-time.Second)
	})
}

func TestPanicsOnTooLongNodeSocketPath(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on node socket path exceeding 107 bytes")
		}
		if !strings.Contains(fmt.Sprint(r), "node socket path") {
			t.Fatalf("expected a node socket path panic, got: %v", r)
		}
	}()

	New(strings.Repeat("a", 36)).
		WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{}).WithEntrypoint(strings.Repeat("b", 36)))
}

func TestPanicsOnTooLongDaemonSetName(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on daemonset name exceeding 63 characters")
		}
		if !strings.Contains(fmt.Sprint(r), "daemonset name") {
			t.Fatalf("expected a daemonset name panic, got: %v", r)
		}
	}()

	New(strings.Repeat("a", 32)).
		WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{}).WithEntrypoint(strings.Repeat("b", 31)))
}

func TestAcceptsLongestNodeSocketPath(t *testing.T) {
	// The longest plugin name keeps the default node socket path at 98
	// bytes, the most the DaemonSet name limit of 63 characters allows.
	p := New(strings.Repeat("a", 63)).
		WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{}))

	if len(p.nodeHooks) != 1 {
		t.Fatalf("expected 1 node hook, got %d", len(p.nodeHooks))
	}
}

func TestAcceptsLongestDaemonSetName(t *testing.T) {
	// Plugin and entrypoint names totalling 62 characters produce a
	// 63-character DaemonSet name, the longest Kubernetes accepts.
	p := New(strings.Repeat("a", 32)).
		WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{}).WithEntrypoint(strings.Repeat("b", 30)))

	if len(p.nodeHooks) != 1 {
		t.Fatalf("expected 1 node hook, got %d", len(p.nodeHooks))
	}
}

func TestPanicsOnDuplicateHookPoint(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate hook point")
		}
	}()

	handler := &stubNodeHandler{}
	New("test").
		WithNodeHook(NodeHookPreVMStart, NodeHandler(handler)).
		WithNodeHook(NodeHookPreVMStart, NodeHandler(handler))
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
		WithNodeHook(NodeHookPreVMStart, NodeHandler(handler)).
		WithNodeHook(NodeHookPreVMStart, NodeHandler(handler).WithEntrypoint("my-plugin"))
}

func TestWithNodeHookDoesNotMutateCallerOption(t *testing.T) {
	handler := &stubNodeHandler{}
	opt := NodeHandler(handler)

	if opt.hookPoint != "" {
		t.Fatal("expected empty hookPoint before WithNodeHook")
	}

	New("test").WithNodeHook(NodeHookPreVMStart, opt)

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

func TestWithEmptyConditionSucceeds(t *testing.T) {
	p := New("test").
		WithCondition("").
		WithDomainHook(ForLibvirt(&stubDomainHandler{}).WithCondition("")).
		WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{}).WithCondition(""))

	if len(p.domainHooks) != 1 || p.domainHooks[0].condition != "" {
		t.Fatal("expected domain hook with empty condition to be registered")
	}

	if len(p.nodeHooks) != 1 || p.nodeHooks[0].condition != "" {
		t.Fatal("expected node hook with empty condition to be registered")
	}
}

func TestWithValidCELConditionSucceeds(t *testing.T) {
	domainOpt := ForLibvirt(&stubDomainHandler{}).WithCondition("vmi.Spec.Domain.CPU.Cores > 2")
	if domainOpt.condition != "vmi.Spec.Domain.CPU.Cores > 2" {
		t.Fatalf("expected domain hook condition to be set, got %q", domainOpt.condition)
	}

	nodeOpt := NodeHandler(&stubNodeHandler{}).WithCondition("vmi.Name == 'test'")
	if nodeOpt.condition != "vmi.Name == 'test'" {
		t.Fatalf("expected node hook condition to be set, got %q", nodeOpt.condition)
	}

	p := New("test").WithCondition("vmi.Name == 'test'")
	if p.condition != "vmi.Name == 'test'" {
		t.Fatalf("expected plugin condition to be set, got %q", p.condition)
	}
}

func TestCELDomainHookCreation(t *testing.T) {
	opt := CELDomainHook("Domain{Name: 'test'}")

	if !opt.isCEL() {
		t.Fatal("expected isCEL to be true")
	}

	if opt.expression != "Domain{Name: 'test'}" {
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
	opt := CELDomainHook("Domain{Name: 'test'}").
		WithCondition("vmi.Name == 'my-vmi'").
		WithFailureStrategy(Ignore).
		WithTimeout(timeout)

	if opt.condition != "vmi.Name == 'my-vmi'" {
		t.Fatalf("expected condition, got %q", opt.condition)
	}

	if opt.failureStrategy == nil || *opt.failureStrategy != Ignore {
		t.Fatal("expected failure strategy Ignore")
	}

	if opt.timeout == nil || *opt.timeout != timeout {
		t.Fatal("expected timeout 30s")
	}
}

func TestPanicsOnEmptyImage(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for empty image")
		}
	}()
	New("test-plugin").WithImage("")
}

func TestPanicsOnEmptyImagePullPolicy(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for empty image pull policy")
		}
	}()
	New("test-plugin").WithImagePullPolicy("")
}

func TestCELDomainHookWithEntrypointPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on CEL hook with entrypoint")
		}
	}()

	CELDomainHook("Domain{Name: 'test'}").WithEntrypoint("my-ep")
}

func TestWithDomainCELHookConvenience(t *testing.T) {
	p := New("test").WithDomainCELHook("Domain{Name: 'test'}")

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
		WithDomainCELHook("Domain{Name: 'test'}").
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
