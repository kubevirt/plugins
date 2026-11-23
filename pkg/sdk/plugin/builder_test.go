package plugin

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

type stubGuestDefinitionHandler struct{}

func (s *stubGuestDefinitionHandler) GuestDefinition(_ context.Context, _ *GuestDefinitionRequest) error {
	return nil
}

type stubPreBootHandler struct{}

func (s *stubPreBootHandler) PreBoot(_ context.Context, _ *PreBootRequest) (*PreBootResponse, error) {
	return nil, nil
}

type stubPreMigrationSourceHandler struct{}

func (s *stubPreMigrationSourceHandler) PreMigrationSource(_ context.Context, _ *PreMigrationSourceRequest) (*PreMigrationSourceResponse, error) {
	return nil, nil
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

func TestLauncherHookConstructors(t *testing.T) {
	guestDefinitionHandler := &stubGuestDefinitionHandler{}
	preBootHandler := &stubPreBootHandler{}
	preMigrationSourceHandler := &stubPreMigrationSourceHandler{}

	guestDefinition := GuestDefinition(guestDefinitionHandler)
	if guestDefinition.hookPoint != LauncherHookGuestDefinition || guestDefinition.guestDefinitionHandler != guestDefinitionHandler {
		t.Fatalf("expected a GuestDefinition hook with the handler, got %+v", guestDefinition)
	}

	preBoot := PreBoot(preBootHandler)
	if preBoot.hookPoint != LauncherHookPreBoot || preBoot.preBootHandler != preBootHandler {
		t.Fatalf("expected a PreBoot hook with the handler, got %+v", preBoot)
	}

	preMigrationSource := PreMigrationSource(preMigrationSourceHandler)
	if preMigrationSource.hookPoint != LauncherHookPreMigrationSource || preMigrationSource.preMigrationSourceHandler != preMigrationSourceHandler {
		t.Fatalf("expected a PreMigrationSource hook with the handler, got %+v", preMigrationSource)
	}

	celHook := GuestDefinitionCEL("Domain{Name: 'test'}")
	if !celHook.isCEL() || celHook.expression != "Domain{Name: 'test'}" || celHook.hookPoint != LauncherHookGuestDefinition {
		t.Fatalf("expected a CEL GuestDefinition hook, got %+v", celHook)
	}
}

func TestWithLauncherHookAddsHook(t *testing.T) {
	handler := &stubGuestDefinitionHandler{}

	p := New("test").WithLauncherHook(GuestDefinition(handler))
	if len(p.launcherHooks) != 1 {
		t.Fatalf("expected 1 launcher hook, got %d", len(p.launcherHooks))
	}

	if p.launcherHooks[0].guestDefinitionHandler != handler {
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

func TestWithEmptyConditionSucceeds(t *testing.T) {
	p := New("test").
		WithCondition("").
		WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{}).WithCondition("")).
		WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{}).WithCondition(""))

	if len(p.launcherHooks) != 1 || p.launcherHooks[0].condition != "" {
		t.Fatal("expected launcher hook with empty condition to be registered")
	}

	if len(p.nodeHooks) != 1 || p.nodeHooks[0].condition != "" {
		t.Fatal("expected node hook with empty condition to be registered")
	}
}

func TestWithFailureStrategySetsPluginLevelDefault(t *testing.T) {
	p := New("test").WithFailureStrategy(Ignore)

	if p.failureStrategy != Ignore {
		t.Fatalf("expected failure strategy %q, got %q", Ignore, p.failureStrategy)
	}
}

func TestLauncherHookPerHookSettings(t *testing.T) {
	timeout := 30 * time.Second

	p := New("test").WithLauncherHook(
		GuestDefinition(&stubGuestDefinitionHandler{}).
			WithCondition("vmi.Spec.Domain.CPU.Cores > 2").
			WithFailureStrategy(Ignore).
			WithTimeout(timeout).
			WithEntrypoint("ep-a"),
	)

	launcherHook := p.launcherHooks[0]

	if launcherHook.condition != "vmi.Spec.Domain.CPU.Cores > 2" {
		t.Fatalf("expected condition, got %q", launcherHook.condition)
	}

	if launcherHook.failureStrategy == nil || *launcherHook.failureStrategy != Ignore {
		t.Fatal("expected failure strategy Ignore")
	}

	if launcherHook.timeout == nil || *launcherHook.timeout != timeout {
		t.Fatalf("expected timeout %v, got %v", timeout, launcherHook.timeout)
	}

	if launcherHook.entrypoint != "ep-a" {
		t.Fatalf("expected entrypoint 'ep-a', got %q", launcherHook.entrypoint)
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
	nodeHandler := &stubNodeHandler{}

	p := New("full-plugin").
		WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{})).
		WithLauncherHook(GuestDefinitionCEL("Domain{Name: 'test'}")).
		WithNodeHook(NodeHookPreVMStart, NodeHandler(nodeHandler)).
		WithNodeHook(NodeHookPostVMStop, NodeHandler(nodeHandler)).
		WithCondition("vmi.Name == 'test'").
		WithFailureStrategy(Ignore)

	if p.name != "full-plugin" {
		t.Fatalf("expected name 'full-plugin', got %q", p.name)
	}

	if len(p.launcherHooks) != 2 {
		t.Fatalf("expected 2 launcher hooks, got %d", len(p.launcherHooks))
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

func TestMultipleLauncherHooks(t *testing.T) {
	p := New("test").
		WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{}).WithEntrypoint("ep-a")).
		WithLauncherHook(PreBoot(&stubPreBootHandler{}).WithEntrypoint("ep-a"))

	if len(p.launcherHooks) != 2 {
		t.Fatalf("expected 2 launcher hooks, got %d", len(p.launcherHooks))
	}

	if p.launcherHooks[0].hookPoint != LauncherHookGuestDefinition || p.launcherHooks[1].hookPoint != LauncherHookPreBoot {
		t.Fatalf("expected GuestDefinition then PreBoot, got %q then %q", p.launcherHooks[0].hookPoint, p.launcherHooks[1].hookPoint)
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
	launcherOpt := GuestDefinition(&stubGuestDefinitionHandler{})
	if launcherOpt.entrypoint != "" {
		t.Fatalf("expected empty launcher hook entrypoint, got %q", launcherOpt.entrypoint)
	}

	nodeOpt := NodeHandler(&stubNodeHandler{})
	if nodeOpt.entrypoint != "" {
		t.Fatalf("expected empty node hook entrypoint, got %q", nodeOpt.entrypoint)
	}
}

func TestSameHookPointOnTwoEntrypoints(t *testing.T) {
	handler := &stubGuestDefinitionHandler{}

	p := New("test").
		WithLauncherHook(GuestDefinition(handler).WithEntrypoint("ep-a")).
		WithLauncherHook(GuestDefinition(handler).WithEntrypoint("ep-b"))

	if len(p.launcherHooks) != 2 {
		t.Fatalf("expected 2 launcher hooks, got %d", len(p.launcherHooks))
	}
}

func TestLauncherAndNodeHooksShareEntrypoint(t *testing.T) {
	p := New("test").
		WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{}).WithEntrypoint("ep-a")).
		WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{}).WithEntrypoint("ep-a"))

	if len(p.launcherHooks) != 1 || len(p.nodeHooks) != 1 {
		t.Fatalf("expected 1 launcher and 1 node hook, got %d and %d", len(p.launcherHooks), len(p.nodeHooks))
	}
}

func TestMultipleCELHooks(t *testing.T) {
	p := New("test").
		WithLauncherHook(GuestDefinitionCEL("Domain{Name: 'a'}")).
		WithLauncherHook(GuestDefinitionCEL("Domain{Name: 'b'}"))

	if len(p.launcherHooks) != 2 {
		t.Fatalf("expected 2 CEL hooks, got %d", len(p.launcherHooks))
	}

	if !p.launcherHooks[0].isCEL() || !p.launcherHooks[1].isCEL() {
		t.Fatal("expected both hooks to be CEL")
	}
}

func TestDuplicateHookPointSameEntrypointPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate hook point within same entrypoint")
		}
	}()

	New("test").
		WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{}).WithEntrypoint("ep-a")).
		WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{}).WithEntrypoint("ep-a"))
}

func TestDuplicateHookPointDifferentHookKindsPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate PreBoot within same entrypoint")
		}
	}()

	New("test").
		WithLauncherHook(PreBoot(&stubPreBootHandler{}).WithEntrypoint("ep-a")).
		WithLauncherHook(PreBoot(&stubPreBootHandler{}).WithEntrypoint("ep-a"))
}

func TestDuplicateHookPointResolvedEntrypointPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when empty and explicit entrypoints resolve to same value")
		}
	}()

	New("my-plugin").
		WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{})).
		WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{}).WithEntrypoint("my-plugin"))
}

func TestPanicsOnNilHandlers(t *testing.T) {
	t.Run("GuestDefinition", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic on nil GuestDefinition handler")
			}
		}()
		GuestDefinition(nil)
	})

	t.Run("PreBoot", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic on nil PreBoot handler")
			}
		}()
		PreBoot(nil)
	})

	t.Run("PreMigrationSource", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic on nil PreMigrationSource handler")
			}
		}()
		PreMigrationSource(nil)
	})
}

func TestPanicsOnNilLauncherHook(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on nil launcher hook")
		}
	}()

	New("test").WithLauncherHook(nil)
}

func TestPanicsOnZeroValueLauncherHook(t *testing.T) {
	t.Run("no hook point", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic on launcher hook without a hook point")
			}
		}()

		New("test").WithLauncherHook(&LauncherHookOption{guestDefinitionHandler: &stubGuestDefinitionHandler{}})
	})

	t.Run("no handler or expression", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic on launcher hook without a handler or CEL expression")
			}
		}()

		New("test").WithLauncherHook(&LauncherHookOption{hookPoint: LauncherHookGuestDefinition})
	})
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

	t.Run("launcher hook", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic on invalid launcher hook failure strategy")
			}
		}()
		GuestDefinition(&stubGuestDefinitionHandler{}).WithFailureStrategy(invalid)
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
	t.Run("launcher hook zero", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic on zero launcher hook timeout")
			}
		}()
		GuestDefinition(&stubGuestDefinitionHandler{}).WithTimeout(0)
	})

	t.Run("launcher hook negative", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic on negative launcher hook timeout")
			}
		}()
		GuestDefinition(&stubGuestDefinitionHandler{}).WithTimeout(-time.Second)
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

func TestPanicsOnTooLongLauncherSocketPath(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on launcher socket path exceeding 107 bytes")
		}
		if !strings.Contains(fmt.Sprint(r), "launcher socket path") {
			t.Fatalf("expected a launcher socket path panic, got: %v", r)
		}
	}()

	New(strings.Repeat("a", 36)).
		WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{}).WithEntrypoint(strings.Repeat("b", 32)))
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

func TestPanicsOnTooLongSidecarContainerName(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on sidecar container name exceeding 63 characters")
		}
		if !strings.Contains(fmt.Sprint(r), "sidecar container name") {
			t.Fatalf("expected a sidecar container name panic, got: %v", r)
		}
	}()

	New(strings.Repeat("a", 32)).
		WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{}).WithEntrypoint(strings.Repeat("b", 16)))
}

func TestAcceptsLongestSidecarContainerName(t *testing.T) {
	// A 32-character plugin name and a 15-character entrypoint produce a
	// 63-character sidecar container name, the longest Kubernetes accepts.
	p := New(strings.Repeat("a", 32)).
		WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{}).WithEntrypoint(strings.Repeat("b", 15)))

	if len(p.launcherHooks) != 1 {
		t.Fatalf("expected 1 launcher hook, got %d", len(p.launcherHooks))
	}
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

func TestPanicsOnTooManyLauncherHooks(t *testing.T) {
	p := New("test")
	for i := 0; i < maxHooksPerPlugin; i++ {
		p.WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{}).WithEntrypoint(fmt.Sprintf("ep-%d", i)))
	}

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on the launcher hook exceeding the CRD bound")
		}
	}()
	p.WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{}).WithEntrypoint("ep-overflow"))
}

func TestPanicsOnTooManyNodeHooks(t *testing.T) {
	p := New("test")
	for i := 0; i < maxHooksPerPlugin; i++ {
		p.WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{}).WithEntrypoint(fmt.Sprintf("ep-%d", i)))
	}

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on the node hook exceeding the CRD bound")
		}
	}()
	p.WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{}).WithEntrypoint("ep-overflow"))
}

func TestPanicsOnDuplicateNodeHookPoint(t *testing.T) {
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

func TestPanicsOnInvalidNamespace(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on invalid namespace")
		}
	}()

	New("test").WithNamespace("My_Namespace")
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

			GuestDefinition(&stubGuestDefinitionHandler{}).WithEntrypoint(tc.input)
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
		opt := GuestDefinition(&stubGuestDefinitionHandler{}).WithEntrypoint(name)

		if opt.entrypoint != name {
			t.Fatalf("expected entrypoint %q, got %q", name, opt.entrypoint)
		}
	}
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

func TestWithLauncherHookDoesNotMutateCallerOption(t *testing.T) {
	opt := PreBoot(&stubPreBootHandler{})

	New("test").WithLauncherHook(opt.WithEntrypoint("ep-a"))

	if opt.entrypoint != "ep-a" {
		t.Fatalf("expected the caller option to keep its own entrypoint, got %q", opt.entrypoint)
	}
}

func TestWithInvalidCELConditionPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on invalid CEL condition for launcher hook")
		}
	}()

	GuestDefinition(&stubGuestDefinitionHandler{}).WithCondition("invalid >>><< expression")
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
	launcherOpt := GuestDefinition(&stubGuestDefinitionHandler{}).WithCondition("vmi.Spec.Domain.CPU.Cores > 2")
	if launcherOpt.condition != "vmi.Spec.Domain.CPU.Cores > 2" {
		t.Fatalf("expected launcher hook condition to be set, got %q", launcherOpt.condition)
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

func TestGuestDefinitionCELEmptyExpressionPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on empty CEL expression")
		}
	}()

	GuestDefinitionCEL("")
}

func TestGuestDefinitionCELInvalidExpressionPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on invalid CEL expression")
		}
	}()

	GuestDefinitionCEL("invalid >>><< expression")
}

func TestGuestDefinitionCELWithPerHookSettings(t *testing.T) {
	opt := GuestDefinitionCEL("Domain{Name: 'test'}").
		WithCondition("vmi.Name == 'my-vmi'").
		WithFailureStrategy(Ignore)

	if opt.condition != "vmi.Name == 'my-vmi'" {
		t.Fatalf("expected condition, got %q", opt.condition)
	}

	if opt.failureStrategy == nil || *opt.failureStrategy != Ignore {
		t.Fatal("expected failure strategy Ignore")
	}
}

func TestGuestDefinitionCELWithTimeoutPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on CEL hook with timeout")
		}
	}()

	GuestDefinitionCEL("Domain{Name: 'test'}").WithTimeout(30 * time.Second)
}

func TestGuestDefinitionCELWithEntrypointPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on CEL hook with entrypoint")
		}
	}()

	GuestDefinitionCEL("Domain{Name: 'test'}").WithEntrypoint("my-ep")
}

func TestMixedSidecarAndCELLauncherHooks(t *testing.T) {
	handler := &stubGuestDefinitionHandler{}
	p := New("test").
		WithLauncherHook(GuestDefinition(handler)).
		WithLauncherHook(GuestDefinitionCEL("Domain{Name: 'test'}")).
		WithLauncherHook(PreBoot(&stubPreBootHandler{}))

	if len(p.launcherHooks) != 3 {
		t.Fatalf("expected 3 launcher hooks, got %d", len(p.launcherHooks))
	}

	if p.launcherHooks[0].isCEL() {
		t.Fatal("expected first hook to be sidecar")
	}

	if !p.launcherHooks[1].isCEL() {
		t.Fatal("expected second hook to be CEL")
	}

	if p.launcherHooks[2].isCEL() {
		t.Fatal("expected third hook to be sidecar")
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

func TestPanicsOnImageWithSingleQuote(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for image containing a single quote")
		}
	}()

	New("test-plugin").WithImage("quay.io/myorg/my-plugin:it's-latest")
}

func TestPanicsOnEmptyImagePullPolicy(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for empty image pull policy")
		}
	}()

	New("test-plugin").WithImagePullPolicy("")
}

func TestWithImagePullPolicyAcceptsKubernetesValues(t *testing.T) {
	for _, policy := range []string{"Always", "IfNotPresent", "Never"} {
		p := New("test-plugin").WithImagePullPolicy(policy)
		if p.imagePullPolicy != policy {
			t.Fatalf("expected image pull policy %q, got %q", policy, p.imagePullPolicy)
		}
	}
}

func TestPanicsOnInvalidImagePullPolicy(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for invalid image pull policy")
		}
	}()

	New("test-plugin").WithImagePullPolicy("Sometimes")
}

func TestWithSidecarPodLabelsSetsLabels(t *testing.T) {
	p := New("test").WithSidecarPodLabels(map[string]string{"example.com/gate": "true", "team": "compute"})

	if len(p.sidecarPodLabels) != 2 || p.sidecarPodLabels["example.com/gate"] != "true" || p.sidecarPodLabels["team"] != "compute" {
		t.Fatalf("expected sidecar pod labels to be set, got %v", p.sidecarPodLabels)
	}
}

func TestPanicsOnEmptySidecarPodLabels(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on empty sidecar pod labels")
		}
	}()

	New("test").WithSidecarPodLabels(map[string]string{})
}

func TestPanicsOnInvalidSidecarPodLabels(t *testing.T) {
	cases := []struct {
		name  string
		key   string
		value string
	}{
		{"empty key", "", "value"},
		{"key with space", "my key", "value"},
		{"key with double quote", `my"key`, "value"},
		{"value with double quote", "gate", `a"b`},
		{"value with backslash", "gate", `a\b`},
		{"key not starting with a letter or digit", "-gate", "value"},
		{"value not ending with a letter or digit", "gate", "value-"},
		{"key too long", strings.Repeat("a", 64), "value"},
		{"value too long", "gate", strings.Repeat("a", 64)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("expected panic for sidecar pod label %q: %q", tc.key, tc.value)
				}
			}()

			New("test").WithSidecarPodLabels(map[string]string{tc.key: tc.value})
		})
	}
}

func TestWithSidecarResourcesSetsResources(t *testing.T) {
	p := New("test").WithSidecarResources(corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("50m")}})

	if len(p.sidecarResources.Requests) != 1 {
		t.Fatalf("expected sidecar resources to be set, got %+v", p.sidecarResources)
	}
}

func TestPreBootRequestAccessors(t *testing.T) {
	req := &PreBootRequest{ArtifactPaths: map[string]string{
		PreBootArtifactCloudInitISO:     "/var/run/kubevirt-ephemeral-disks/cloud-init-data/default/vmi/noCloud.iso",
		PreBootArtifactEphemeralDiskDir: "/var/run/kubevirt-ephemeral-disks",
	}}

	isoPath, ok := req.CloudInitISO()
	if !ok || isoPath != "/var/run/kubevirt-ephemeral-disks/cloud-init-data/default/vmi/noCloud.iso" {
		t.Fatalf("expected cloud-init ISO path, got %q (%v)", isoPath, ok)
	}

	diskDir, ok := req.EphemeralDiskDir()
	if !ok || diskDir != "/var/run/kubevirt-ephemeral-disks" {
		t.Fatalf("expected ephemeral disk dir, got %q (%v)", diskDir, ok)
	}
}

func TestPreBootRequestAccessorsAbsentKeys(t *testing.T) {
	req := &PreBootRequest{ArtifactPaths: map[string]string{}}

	if path, ok := req.CloudInitISO(); ok {
		t.Fatalf("expected no cloud-init ISO, got %q", path)
	}

	if path, ok := req.EphemeralDiskDir(); ok {
		t.Fatalf("expected no ephemeral disk dir, got %q", path)
	}
}

func TestSocketPaths(t *testing.T) {
	if got := LauncherSocketPath("my-plugin", ""); got != "/var/run/kubevirt-plugin/my-plugin/launcher.sock" {
		t.Fatalf("expected default launcher socket path, got %q", got)
	}

	if got := LauncherSocketPath("my-plugin", "my-plugin"); got != "/var/run/kubevirt-plugin/my-plugin/launcher.sock" {
		t.Fatalf("expected plugin-name entrypoint to use the default launcher socket path, got %q", got)
	}

	if got := LauncherSocketPath("my-plugin", "ep-a"); got != "/var/run/kubevirt-plugin/my-plugin/ep-a/launcher.sock" {
		t.Fatalf("expected entrypoint launcher socket path, got %q", got)
	}

	if got := NodeSocketPath("my-plugin", ""); got != "/var/run/kubevirt/plugins/my-plugin/node.sock" {
		t.Fatalf("expected default node socket path, got %q", got)
	}

	if got := NodeSocketPath("my-plugin", "ep-a"); got != "/var/run/kubevirt/plugins/my-plugin/ep-a/node.sock" {
		t.Fatalf("expected entrypoint node socket path, got %q", got)
	}
}
