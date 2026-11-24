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

// assertPanics fails the test unless fn panics; a non-empty wantMsg must be
// contained in the panic value.
func assertPanics(t *testing.T, wantMsg string, fn func()) {
	t.Helper()

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic")
		}
		if wantMsg != "" && !strings.Contains(fmt.Sprint(r), wantMsg) {
			t.Fatalf("expected panic containing %q, got: %v", wantMsg, r)
		}
	}()
	fn()
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

func TestDuplicateHookPointPanics(t *testing.T) {
	tests := []struct {
		name string
		hook func()
	}{
		{"same entrypoint", func() {
			New("test").
				WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{}).WithEntrypoint("ep-a")).
				WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{}).WithEntrypoint("ep-a"))
		}},
		{"same entrypoint, PreBoot registered twice", func() {
			New("test").
				WithLauncherHook(PreBoot(&stubPreBootHandler{}).WithEntrypoint("ep-a")).
				WithLauncherHook(PreBoot(&stubPreBootHandler{}).WithEntrypoint("ep-a"))
		}},
		{"empty entrypoint resolving to the plugin name", func() {
			New("my-plugin").
				WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{})).
				WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{}).WithEntrypoint("my-plugin"))
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertPanics(t, "", test.hook)
		})
	}
}

func TestPanicsOnNilHandlers(t *testing.T) {
	t.Run("GuestDefinition", func(t *testing.T) {
		assertPanics(t, "", func() { GuestDefinition(nil) })
	})

	t.Run("PreBoot", func(t *testing.T) {
		assertPanics(t, "", func() { PreBoot(nil) })
	})

	t.Run("PreMigrationSource", func(t *testing.T) {
		assertPanics(t, "", func() { PreMigrationSource(nil) })
	})
}

func TestPanicsOnNilLauncherHook(t *testing.T) {
	assertPanics(t, "", func() { New("test").WithLauncherHook(nil) })
}

func TestPanicsOnZeroValueLauncherHook(t *testing.T) {
	t.Run("no hook point", func(t *testing.T) {
		assertPanics(t, "", func() {
			New("test").WithLauncherHook(&LauncherHookOption{guestDefinitionHandler: &stubGuestDefinitionHandler{}})
		})
	})

	t.Run("no handler or expression", func(t *testing.T) {
		assertPanics(t, "", func() {
			New("test").WithLauncherHook(&LauncherHookOption{hookPoint: LauncherHookGuestDefinition})
		})
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
			assertPanics(t, "", func() { New(tc.input) })
		})
	}
}

func TestPanicsOnEmptyName(t *testing.T) {
	assertPanics(t, "", func() { New("") })
}

func TestPanicsOnNilNodeHandler(t *testing.T) {
	assertPanics(t, "", func() { NodeHandler(nil) })
}

func TestPanicsOnNodeHookWithoutHandler(t *testing.T) {
	assertPanics(t, "", func() { New("test").WithNodeHook(NodeHookPreVMStart, &NodeHookOption{}) })
}

func TestPanicsOnInvalidHookPoint(t *testing.T) {
	assertPanics(t, "", func() {
		New("test").WithNodeHook(NodeHookPoint("InvalidHookPoint"), NodeHandler(&stubNodeHandler{}))
	})
}

func TestPanicsOnInvalidFailureStrategy(t *testing.T) {
	invalid := FailureStrategy("BestEffort")

	t.Run("plugin level", func(t *testing.T) {
		assertPanics(t, "", func() { New("test").WithFailureStrategy(invalid) })
	})

	t.Run("launcher hook", func(t *testing.T) {
		assertPanics(t, "", func() { GuestDefinition(&stubGuestDefinitionHandler{}).WithFailureStrategy(invalid) })
	})

	t.Run("node hook", func(t *testing.T) {
		assertPanics(t, "", func() { NodeHandler(&stubNodeHandler{}).WithFailureStrategy(invalid) })
	})
}

func TestPanicsOnNonPositiveTimeout(t *testing.T) {
	t.Run("launcher hook", func(t *testing.T) {
		assertPanics(t, "", func() { GuestDefinition(&stubGuestDefinitionHandler{}).WithTimeout(0) })
	})

	t.Run("node hook", func(t *testing.T) {
		assertPanics(t, "", func() { NodeHandler(&stubNodeHandler{}).WithTimeout(0) })
	})
}

func TestPanicsOnTooLongNodeSocketPath(t *testing.T) {
	assertPanics(t, "node socket path", func() {
		New(strings.Repeat("a", 36)).
			WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{}).WithEntrypoint(strings.Repeat("b", 36)))
	})
}

func TestPanicsOnTooLongLauncherSocketPath(t *testing.T) {
	assertPanics(t, "launcher socket path", func() {
		New(strings.Repeat("a", 36)).
			WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{}).WithEntrypoint(strings.Repeat("b", 32)))
	})
}

func TestPanicsOnTooLongDaemonSetName(t *testing.T) {
	assertPanics(t, "daemonset name", func() {
		New(strings.Repeat("a", 32)).
			WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{}).WithEntrypoint(strings.Repeat("b", 31)))
	})
}

func TestPanicsOnTooLongSidecarContainerName(t *testing.T) {
	assertPanics(t, "sidecar container name", func() {
		New(strings.Repeat("a", 32)).
			WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{}).WithEntrypoint(strings.Repeat("b", 16)))
	})
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
	assertPanics(t, "", func() {
		p := New("test")
		for i := 0; i < maxHooksPerPlugin; i++ {
			p.WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{}).WithEntrypoint(fmt.Sprintf("ep-%d", i)))
		}
		p.WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{}).WithEntrypoint("ep-overflow"))
	})
}

func TestPanicsOnTooManyNodeHooks(t *testing.T) {
	assertPanics(t, "", func() {
		p := New("test")
		for i := 0; i < maxHooksPerPlugin; i++ {
			p.WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{}).WithEntrypoint(fmt.Sprintf("ep-%d", i)))
		}
		p.WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{}).WithEntrypoint("ep-overflow"))
	})
}

func TestPanicsOnDuplicateNodeHookPoint(t *testing.T) {
	assertPanics(t, "", func() {
		handler := &stubNodeHandler{}
		New("test").
			WithNodeHook(NodeHookPreVMStart, NodeHandler(handler)).
			WithNodeHook(NodeHookPreVMStart, NodeHandler(handler))
	})
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
	assertPanics(t, "", func() { New("test").WithNamespace("") })
}

func TestPanicsOnInvalidNamespace(t *testing.T) {
	assertPanics(t, "", func() { New("test").WithNamespace("My_Namespace") })
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
			assertPanics(t, "", func() { GuestDefinition(&stubGuestDefinitionHandler{}).WithEntrypoint(tc.input) })
		})
	}
}

func TestEntrypointValidationNodeHookPanics(t *testing.T) {
	assertPanics(t, "", func() { NodeHandler(&stubNodeHandler{}).WithEntrypoint("INVALID") })
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

func TestWithLauncherHookCopiesTheCallerOption(t *testing.T) {
	opt := PreBoot(&stubPreBootHandler{})

	p := New("test").WithLauncherHook(opt.WithEntrypoint("ep-a"))

	opt.entrypoint = "ep-b"

	if p.launcherHooks[0].entrypoint != "ep-a" {
		t.Fatalf("expected the registered hook to keep entrypoint %q, got %q", "ep-a", p.launcherHooks[0].entrypoint)
	}
}

func TestWithInvalidCELConditionPanics(t *testing.T) {
	assertPanics(t, "", func() { GuestDefinition(&stubGuestDefinitionHandler{}).WithCondition("invalid >>><< expression") })
}

func TestWithInvalidCELConditionOnNodeHookPanics(t *testing.T) {
	assertPanics(t, "", func() { NodeHandler(&stubNodeHandler{}).WithCondition("invalid >>><< expression") })
}

func TestWithInvalidPluginLevelCELConditionPanics(t *testing.T) {
	assertPanics(t, "", func() { New("test").WithCondition("invalid >>><< expression") })
}

func TestGuestDefinitionCELEmptyExpressionPanics(t *testing.T) {
	assertPanics(t, "", func() { GuestDefinitionCEL("") })
}

func TestGuestDefinitionCELInvalidExpressionPanics(t *testing.T) {
	assertPanics(t, "", func() { GuestDefinitionCEL("invalid >>><< expression") })
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
	assertPanics(t, "", func() { GuestDefinitionCEL("Domain{Name: 'test'}").WithTimeout(30 * time.Second) })
}

func TestGuestDefinitionCELWithEntrypointPanics(t *testing.T) {
	assertPanics(t, "", func() { GuestDefinitionCEL("Domain{Name: 'test'}").WithEntrypoint("my-ep") })
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
	assertPanics(t, "", func() { New("test-plugin").WithImage("") })
}

func TestPanicsOnImageWithSingleQuote(t *testing.T) {
	assertPanics(t, "", func() { New("test-plugin").WithImage("quay.io/myorg/my-plugin:it's-latest") })
}

func TestPanicsOnEmptyImagePullPolicy(t *testing.T) {
	assertPanics(t, "", func() { New("test-plugin").WithImagePullPolicy("") })
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
	assertPanics(t, "", func() { New("test-plugin").WithImagePullPolicy("Sometimes") })
}

func TestWithSidecarPodLabelsSetsLabels(t *testing.T) {
	p := New("test").WithSidecarPodLabels(map[string]string{"example.com/gate": "true", "team": "compute"})

	if len(p.sidecarPodLabels) != 2 || p.sidecarPodLabels["example.com/gate"] != "true" || p.sidecarPodLabels["team"] != "compute" {
		t.Fatalf("expected sidecar pod labels to be set, got %v", p.sidecarPodLabels)
	}
}

func TestPanicsOnEmptySidecarPodLabels(t *testing.T) {
	assertPanics(t, "", func() { New("test").WithSidecarPodLabels(map[string]string{}) })
}

func TestWithSidecarPodLabelsAcceptsEmptyValue(t *testing.T) {
	p := New("test").WithSidecarPodLabels(map[string]string{"gate": ""})

	if len(p.sidecarPodLabels) != 1 || p.sidecarPodLabels["gate"] != "" {
		t.Fatalf("expected sidecar pod labels to be set, got %v", p.sidecarPodLabels)
	}
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
		{"empty name after prefix", "example.com/", "value"},
		{"empty prefix", "/gate", "value"},
		{"prefix starting with a hyphen", "-a/gate", "value"},
		{"prefix with consecutive dots", "a..b/gate", "value"},
		{"uppercase prefix", "Example.com/gate", "value"},
		{"prefix too long", strings.Repeat("a", 254) + "/gate", "value"},
		{"key with non-ASCII character", "a\u0161b", "value"},
		{"value with non-ASCII character", "gate", "a\u0161b"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The panic message embeds the offending key %q-escaped; the
			// empty key escapes to the empty string and matches any panic.
			wantMsg := strings.Trim(fmt.Sprintf("%q", tc.key), `"`)
			assertPanics(t, wantMsg, func() { New("test").WithSidecarPodLabels(map[string]string{tc.key: tc.value}) })
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
