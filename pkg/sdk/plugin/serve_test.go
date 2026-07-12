package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pb "github.com/iholder101/kubevirt-plugins/api/hooks/v1alpha1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	v1 "kubevirt.io/api/core/v1"
	"libvirt.org/go/libvirtxml"
)

type mutatingDomainHandler struct {
	receivedVMI *v1.VirtualMachineInstance
}

func (h *mutatingDomainHandler) MutateDomain(ctx context.Context, domain *libvirtxml.Domain, vmi *v1.VirtualMachineInstance) error {
	h.receivedVMI = vmi
	domain.Name = "mutated-" + domain.Name
	return nil
}

type contextRecordingHandler struct {
	receivedInvocationContext string
}

func (h *contextRecordingHandler) MutateDomain(ctx context.Context, domain *libvirtxml.Domain, _ *v1.VirtualMachineInstance) error {
	h.receivedInvocationContext = InvocationContextFromContext(ctx)
	return nil
}

type errorDomainHandler struct {
	err error
}

func (h *errorDomainHandler) MutateDomain(_ context.Context, _ *libvirtxml.Domain, _ *v1.VirtualMachineInstance) error {
	return h.err
}

type errorNodeHandler struct {
	err error
}

func (h *errorNodeHandler) ExecuteNodeHook(_ context.Context, _ *NodeHookRequest) error {
	return h.err
}

type recordingNodeHandler struct {
	receivedHookPoint string
	receivedNodeName  string
	receivedVMI       *v1.VirtualMachineInstance
}

func (h *recordingNodeHandler) ExecuteNodeHook(_ context.Context, req *NodeHookRequest) error {
	h.receivedHookPoint = req.HookPoint
	h.receivedNodeName = req.NodeName
	h.receivedVMI = req.VMI
	return nil
}

func mustMarshalVMI(t *testing.T, vmi *v1.VirtualMachineInstance) []byte {
	t.Helper()

	data, err := json.Marshal(vmi)
	if err != nil {
		t.Fatalf("failed to marshal VMI: %v", err)
	}

	return data
}

func testVMI(name string) *v1.VirtualMachineInstance {
	vmi := &v1.VirtualMachineInstance{}
	vmi.Name = name
	return vmi
}

func testDomainXML(t *testing.T, name string) []byte {
	t.Helper()

	d := &libvirtxml.Domain{Type: "kvm", Name: name}
	xmlStr, err := d.Marshal()
	if err != nil {
		t.Fatalf("failed to marshal domain XML: %v", err)
	}

	return []byte(xmlStr)
}

func waitForSocket(t *testing.T, path string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("unix", path, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("socket %s did not become connectable within timeout", path)
}

func dialSocket(t *testing.T, sockPath string) *grpc.ClientConn {
	t.Helper()

	conn, err := grpc.NewClient("unix:"+sockPath, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to connect to %s: %v", sockPath, err)
	}

	t.Cleanup(func() { conn.Close() })
	return conn
}

func awaitServeStop(t *testing.T, errCh <-chan error) {
	t.Helper()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("serve returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not stop within timeout")
	}
}

// --- Adapter unit tests ---

func TestDomainHookAdapterMutatesDomain(t *testing.T) {
	handler := &mutatingDomainHandler{}
	adapter := &domainHookAdapter{handler: handler}

	vmi := testVMI("test-vmi")
	resp, err := adapter.MutateDomain(context.Background(), &pb.MutateDomainRequest{
		DomainType:     "libvirt",
		Domain:         testDomainXML(t, "original"),
		Vmi:            mustMarshalVMI(t, vmi),
		SidecarContext: &pb.SidecarContext{},
	})
	if err != nil {
		t.Fatalf("MutateDomain failed: %v", err)
	}

	var result libvirtxml.Domain
	if err := result.Unmarshal(string(resp.Domain)); err != nil {
		t.Fatalf("failed to unmarshal response domain: %v", err)
	}

	if result.Name != "mutated-original" {
		t.Fatalf("expected domain name %q, got %q", "mutated-original", result.Name)
	}

	if handler.receivedVMI == nil || handler.receivedVMI.Name != "test-vmi" {
		t.Fatal("handler did not receive the correct VMI")
	}
}

func TestDomainHookAdapterPassthroughUnknownDomainType(t *testing.T) {
	handler := &mutatingDomainHandler{}
	adapter := &domainHookAdapter{handler: handler}

	originalDomain := []byte("<some-unknown-format/>")
	resp, err := adapter.MutateDomain(context.Background(), &pb.MutateDomainRequest{
		DomainType: "mshv",
		Domain:     originalDomain,
		Vmi:        mustMarshalVMI(t, testVMI("test")),
	})
	if err != nil {
		t.Fatalf("MutateDomain failed: %v", err)
	}

	if string(resp.Domain) != string(originalDomain) {
		t.Fatalf("expected domain bytes unchanged, got %q", string(resp.Domain))
	}
}

func TestDomainHookAdapterPassesInvocationContext(t *testing.T) {
	handler := &contextRecordingHandler{}
	adapter := &domainHookAdapter{handler: handler}

	_, err := adapter.MutateDomain(context.Background(), &pb.MutateDomainRequest{
		DomainType:     "libvirt",
		Domain:         testDomainXML(t, "test"),
		Vmi:            mustMarshalVMI(t, testVMI("test")),
		SidecarContext: &pb.SidecarContext{InvocationContext: "my-invocation-context"},
	})
	if err != nil {
		t.Fatalf("MutateDomain failed: %v", err)
	}

	if handler.receivedInvocationContext != "my-invocation-context" {
		t.Fatalf("expected invocation context %q, got %q", "my-invocation-context", handler.receivedInvocationContext)
	}
}

func TestDomainHookAdapterNilSidecarContext(t *testing.T) {
	handler := &contextRecordingHandler{}
	adapter := &domainHookAdapter{handler: handler}

	_, err := adapter.MutateDomain(context.Background(), &pb.MutateDomainRequest{
		DomainType: "libvirt",
		Domain:     testDomainXML(t, "test"),
		Vmi:        mustMarshalVMI(t, testVMI("test")),
	})
	if err != nil {
		t.Fatalf("MutateDomain failed: %v", err)
	}

	if handler.receivedInvocationContext != "" {
		t.Fatalf("expected empty invocation context, got %q", handler.receivedInvocationContext)
	}
}

func TestNodeHookAdapterCallsHandler(t *testing.T) {
	handler := &recordingNodeHandler{}
	adapter := &nodeHookAdapter{handlers: map[string]NodeHookHandler{PreVMStart: handler}}

	vmi := testVMI("node-vmi")
	_, err := adapter.ExecuteNodeHook(context.Background(), &pb.ExecuteNodeHookRequest{
		HookPoint:   PreVMStart,
		Vmi:         mustMarshalVMI(t, vmi),
		NodeContext: &pb.NodeContext{NodeName: "worker-1"},
	})
	if err != nil {
		t.Fatalf("ExecuteNodeHook failed: %v", err)
	}

	if handler.receivedHookPoint != PreVMStart {
		t.Fatalf("expected hook point %q, got %q", PreVMStart, handler.receivedHookPoint)
	}

	if handler.receivedNodeName != "worker-1" {
		t.Fatalf("expected node name %q, got %q", "worker-1", handler.receivedNodeName)
	}

	if handler.receivedVMI == nil || handler.receivedVMI.Name != "node-vmi" {
		t.Fatal("handler did not receive the correct VMI")
	}
}

func TestNodeHookAdapterNilNodeContext(t *testing.T) {
	handler := &recordingNodeHandler{}
	adapter := &nodeHookAdapter{handlers: map[string]NodeHookHandler{PreVMStart: handler}}

	_, err := adapter.ExecuteNodeHook(context.Background(), &pb.ExecuteNodeHookRequest{
		HookPoint: PreVMStart,
		Vmi:       mustMarshalVMI(t, testVMI("test")),
	})
	if err != nil {
		t.Fatalf("ExecuteNodeHook failed: %v", err)
	}

	if handler.receivedNodeName != "" {
		t.Fatalf("expected empty node name, got %q", handler.receivedNodeName)
	}
}

// --- Integration tests ---

func TestServeStartsAndStopsGracefully(t *testing.T) {
	tmpDir := t.TempDir()
	domainSock := filepath.Join(tmpDir, "domain.sock")

	handler := &mutatingDomainHandler{}
	p := New("test-plugin").WithDomainHook(ForLibvirt(handler))

	stopCh := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- p.Serve(withDomainSocketPath(domainSock), withStopCh(stopCh))
	}()

	waitForSocket(t, domainSock)

	close(stopCh)
	awaitServeStop(t, errCh)
}

func TestServeRegistersDomainHookAdapter(t *testing.T) {
	tmpDir := t.TempDir()
	domainSock := filepath.Join(tmpDir, "domain.sock")

	handler := &mutatingDomainHandler{}
	p := New("test-plugin").WithDomainHook(ForLibvirt(handler))

	stopCh := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- p.Serve(withDomainSocketPath(domainSock), withStopCh(stopCh))
	}()

	waitForSocket(t, domainSock)

	conn := dialSocket(t, domainSock)
	client := pb.NewDomainHookServiceClient(conn)

	resp, err := client.MutateDomain(context.Background(), &pb.MutateDomainRequest{
		DomainType:     "libvirt",
		Domain:         testDomainXML(t, "original"),
		Vmi:            mustMarshalVMI(t, testVMI("grpc-test-vmi")),
		SidecarContext: &pb.SidecarContext{},
	})
	if err != nil {
		t.Fatalf("MutateDomain RPC failed: %v", err)
	}

	var result libvirtxml.Domain
	if err := result.Unmarshal(string(resp.Domain)); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if result.Name != "mutated-original" {
		t.Fatalf("expected mutated domain name, got %q", result.Name)
	}

	close(stopCh)
	awaitServeStop(t, errCh)
}

func TestServeRegistersNodeHookAdapter(t *testing.T) {
	tmpDir := t.TempDir()
	nodeSock := filepath.Join(tmpDir, "node.sock")

	handler := &recordingNodeHandler{}
	p := New("test-plugin").WithNodeHook(PreVMStart, NodeHandler(handler))

	stopCh := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- p.Serve(withNodeSocketPath(nodeSock), withStopCh(stopCh))
	}()

	waitForSocket(t, nodeSock)

	conn := dialSocket(t, nodeSock)
	client := pb.NewNodeHookServiceClient(conn)

	_, err := client.ExecuteNodeHook(context.Background(), &pb.ExecuteNodeHookRequest{
		HookPoint:   PreVMStart,
		Vmi:         mustMarshalVMI(t, testVMI("node-test-vmi")),
		NodeContext: &pb.NodeContext{NodeName: "test-node"},
	})
	if err != nil {
		t.Fatalf("ExecuteNodeHook RPC failed: %v", err)
	}

	if handler.receivedHookPoint != PreVMStart {
		t.Fatalf("expected hook point %q, got %q", PreVMStart, handler.receivedHookPoint)
	}

	if handler.receivedNodeName != "test-node" {
		t.Fatalf("expected node name %q, got %q", "test-node", handler.receivedNodeName)
	}

	close(stopCh)
	awaitServeStop(t, errCh)
}

// --- Adapter error-path tests ---

func TestDomainHookAdapterInvalidXML(t *testing.T) {
	adapter := &domainHookAdapter{handler: &mutatingDomainHandler{}}

	_, err := adapter.MutateDomain(context.Background(), &pb.MutateDomainRequest{
		DomainType:     "libvirt",
		Domain:         []byte("not-valid-xml{{{"),
		Vmi:            mustMarshalVMI(t, testVMI("test")),
		SidecarContext: &pb.SidecarContext{},
	})
	if err == nil {
		t.Fatal("expected error for invalid domain XML")
	}

	if !strings.Contains(err.Error(), "unmarshal domain XML") {
		t.Fatalf("expected domain XML unmarshal error, got: %v", err)
	}
}

func TestDomainHookAdapterInvalidVMIJSON(t *testing.T) {
	adapter := &domainHookAdapter{handler: &mutatingDomainHandler{}}

	_, err := adapter.MutateDomain(context.Background(), &pb.MutateDomainRequest{
		DomainType:     "libvirt",
		Domain:         testDomainXML(t, "test"),
		Vmi:            []byte("not-valid-json{{{"),
		SidecarContext: &pb.SidecarContext{},
	})
	if err == nil {
		t.Fatal("expected error for invalid VMI JSON")
	}

	if !strings.Contains(err.Error(), "unmarshal VMI") {
		t.Fatalf("expected VMI unmarshal error, got: %v", err)
	}
}

func TestDomainHookAdapterHandlerError(t *testing.T) {
	handlerErr := fmt.Errorf("handler broke")
	adapter := &domainHookAdapter{handler: &errorDomainHandler{err: handlerErr}}

	_, err := adapter.MutateDomain(context.Background(), &pb.MutateDomainRequest{
		DomainType:     "libvirt",
		Domain:         testDomainXML(t, "test"),
		Vmi:            mustMarshalVMI(t, testVMI("test")),
		SidecarContext: &pb.SidecarContext{},
	})
	if err == nil {
		t.Fatal("expected error when handler fails")
	}

	if !strings.Contains(err.Error(), "handler broke") {
		t.Fatalf("expected handler error to be propagated, got: %v", err)
	}
}

func TestNodeHookAdapterUnregisteredHookPoint(t *testing.T) {
	adapter := &nodeHookAdapter{handlers: map[string]NodeHookHandler{
		PreVMStart: &recordingNodeHandler{},
	}}

	_, err := adapter.ExecuteNodeHook(context.Background(), &pb.ExecuteNodeHookRequest{
		HookPoint:   "NonExistentHookPoint",
		Vmi:         mustMarshalVMI(t, testVMI("test")),
		NodeContext: &pb.NodeContext{NodeName: "node-1"},
	})
	if err == nil {
		t.Fatal("expected error for unregistered hook point")
	}

	if !strings.Contains(err.Error(), "no handler registered") {
		t.Fatalf("expected unregistered hook point error, got: %v", err)
	}
}

func TestNodeHookAdapterInvalidVMIJSON(t *testing.T) {
	adapter := &nodeHookAdapter{handlers: map[string]NodeHookHandler{
		PreVMStart: &recordingNodeHandler{},
	}}

	_, err := adapter.ExecuteNodeHook(context.Background(), &pb.ExecuteNodeHookRequest{
		HookPoint:   PreVMStart,
		Vmi:         []byte("not-valid-json{{{"),
		NodeContext: &pb.NodeContext{NodeName: "node-1"},
	})
	if err == nil {
		t.Fatal("expected error for invalid VMI JSON")
	}

	if !strings.Contains(err.Error(), "unmarshal VMI") {
		t.Fatalf("expected VMI unmarshal error, got: %v", err)
	}
}

func TestNodeHookAdapterHandlerError(t *testing.T) {
	handlerErr := fmt.Errorf("node handler broke")
	adapter := &nodeHookAdapter{handlers: map[string]NodeHookHandler{
		PreVMStart: &errorNodeHandler{err: handlerErr},
	}}

	_, err := adapter.ExecuteNodeHook(context.Background(), &pb.ExecuteNodeHookRequest{
		HookPoint:   PreVMStart,
		Vmi:         mustMarshalVMI(t, testVMI("test")),
		NodeContext: &pb.NodeContext{NodeName: "node-1"},
	})
	if err == nil {
		t.Fatal("expected error when handler fails")
	}

	if !strings.Contains(err.Error(), "node handler broke") {
		t.Fatalf("expected handler error to be propagated, got: %v", err)
	}
}

type appendingDomainHandler struct {
	suffix string
	called bool
}

func (h *appendingDomainHandler) MutateDomain(_ context.Context, domain *libvirtxml.Domain, _ *v1.VirtualMachineInstance) error {
	h.called = true
	domain.Name = domain.Name + h.suffix
	return nil
}

func TestServeWithEntrypointFiltersDomainHooks(t *testing.T) {
	tmpDir := t.TempDir()
	domainSock := filepath.Join(tmpDir, "domain.sock")

	handlerA := &appendingDomainHandler{suffix: "-A"}
	handlerB := &appendingDomainHandler{suffix: "-B"}

	p := New("test-plugin").
		WithDomainHook(ForLibvirt(handlerA).WithEntrypoint("ep-a")).
		WithDomainHook(ForLibvirt(handlerB).WithEntrypoint("ep-b"))

	stopCh := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- p.Serve(withDomainSocketPath(domainSock), withStopCh(stopCh), withEntrypoint("ep-a"))
	}()

	waitForSocket(t, domainSock)

	conn := dialSocket(t, domainSock)
	client := pb.NewDomainHookServiceClient(conn)

	resp, err := client.MutateDomain(context.Background(), &pb.MutateDomainRequest{
		DomainType:     "libvirt",
		Domain:         testDomainXML(t, "original"),
		Vmi:            mustMarshalVMI(t, testVMI("test")),
		SidecarContext: &pb.SidecarContext{},
	})
	if err != nil {
		t.Fatalf("MutateDomain RPC failed: %v", err)
	}

	var result libvirtxml.Domain
	if err := result.Unmarshal(string(resp.Domain)); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if result.Name != "original-A" {
		t.Fatalf("expected domain name 'original-A', got %q", result.Name)
	}

	if !handlerA.called {
		t.Fatal("expected handler A to be called")
	}

	if handlerB.called {
		t.Fatal("expected handler B to NOT be called")
	}

	close(stopCh)
	awaitServeStop(t, errCh)
}

func TestServeWithEntrypointFiltersNodeHooks(t *testing.T) {
	tmpDir := t.TempDir()
	nodeSock := filepath.Join(tmpDir, "node.sock")

	handlerA := &recordingNodeHandler{}
	handlerB := &recordingNodeHandler{}

	p := New("test-plugin").
		WithNodeHook(PreVMStart, NodeHandler(handlerA).WithEntrypoint("ep-a")).
		WithNodeHook(PostVMStop, NodeHandler(handlerB).WithEntrypoint("ep-b"))

	stopCh := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- p.Serve(withNodeSocketPath(nodeSock), withStopCh(stopCh), withEntrypoint("ep-a"))
	}()

	waitForSocket(t, nodeSock)

	conn := dialSocket(t, nodeSock)
	client := pb.NewNodeHookServiceClient(conn)

	_, err := client.ExecuteNodeHook(context.Background(), &pb.ExecuteNodeHookRequest{
		HookPoint:   PreVMStart,
		Vmi:         mustMarshalVMI(t, testVMI("test")),
		NodeContext: &pb.NodeContext{NodeName: "worker-1"},
	})
	if err != nil {
		t.Fatalf("ExecuteNodeHook RPC failed: %v", err)
	}

	if handlerA.receivedHookPoint != PreVMStart {
		t.Fatalf("expected handler A to receive %q, got %q", PreVMStart, handlerA.receivedHookPoint)
	}

	if handlerB.receivedHookPoint != "" {
		t.Fatal("expected handler B to NOT be called")
	}

	close(stopCh)
	awaitServeStop(t, errCh)
}

func TestServeMixedEntrypointsWithoutFlagErrors(t *testing.T) {
	p := New("test-plugin").
		WithNodeHook(PreVMStart, NodeHandler(&stubNodeHandler{}).WithEntrypoint("ep-a")).
		WithNodeHook(PostVMStop, NodeHandler(&stubNodeHandler{}).WithEntrypoint("ep-b"))

	err := p.Serve()
	if err == nil {
		t.Fatal("expected error when serving multiple entrypoints without --entrypoint")
	}

	if !strings.Contains(err.Error(), "multiple entrypoints") {
		t.Fatalf("expected 'multiple entrypoints' error, got: %v", err)
	}
}

func TestServeSingleEntrypointNoFlagOK(t *testing.T) {
	tmpDir := t.TempDir()
	domainSock := filepath.Join(tmpDir, "domain.sock")

	p := New("test-plugin").WithDomainHook(ForLibvirt(&stubDomainHandler{}))

	stopCh := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- p.Serve(withDomainSocketPath(domainSock), withStopCh(stopCh))
	}()

	waitForSocket(t, domainSock)

	close(stopCh)
	awaitServeStop(t, errCh)
}

func TestServeMultipleDomainHooksSameEntrypoint(t *testing.T) {
	tmpDir := t.TempDir()
	domainSock := filepath.Join(tmpDir, "domain.sock")

	handler1 := &appendingDomainHandler{suffix: "-first"}
	handler2 := &appendingDomainHandler{suffix: "-second"}

	p := New("test-plugin").
		WithDomainHook(ForLibvirt(handler1)).
		WithDomainHook(ForLibvirt(handler2))

	stopCh := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- p.Serve(withDomainSocketPath(domainSock), withStopCh(stopCh))
	}()

	waitForSocket(t, domainSock)

	conn := dialSocket(t, domainSock)
	client := pb.NewDomainHookServiceClient(conn)

	resp, err := client.MutateDomain(context.Background(), &pb.MutateDomainRequest{
		DomainType:     "libvirt",
		Domain:         testDomainXML(t, "original"),
		Vmi:            mustMarshalVMI(t, testVMI("test")),
		SidecarContext: &pb.SidecarContext{},
	})
	if err != nil {
		t.Fatalf("MutateDomain RPC failed: %v", err)
	}

	var result libvirtxml.Domain
	if err := result.Unmarshal(string(resp.Domain)); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if result.Name != "original-first-second" {
		t.Fatalf("expected chained mutation 'original-first-second', got %q", result.Name)
	}

	if !handler1.called || !handler2.called {
		t.Fatal("expected both handlers to be called")
	}

	close(stopCh)
	awaitServeStop(t, errCh)
}

func TestServeWithNoHooks(t *testing.T) {
	p := New("no-hooks-plugin")

	err := p.Serve()
	if err == nil {
		t.Fatal("expected error when serving with no hooks")
	}

	if !strings.Contains(err.Error(), "no hooks registered") {
		t.Fatalf("expected 'no hooks registered' error, got: %v", err)
	}
}

func TestChainedDomainHookFirstErrorStopsChain(t *testing.T) {
	errHandler := &errorDomainHandler{err: fmt.Errorf("first handler failed")}
	secondHandler := &appendingDomainHandler{suffix: "-second"}

	chained := &chainedDomainHandler{handlers: []LibvirtDomainHookHandler{errHandler, secondHandler}}

	domain := &libvirtxml.Domain{Name: "test"}
	err := chained.MutateDomain(context.Background(), domain, testVMI("test"))

	if err == nil {
		t.Fatal("expected error from first handler")
	}

	if !strings.Contains(err.Error(), "first handler failed") {
		t.Fatalf("expected first handler error, got: %v", err)
	}

	if secondHandler.called {
		t.Fatal("expected second handler to NOT be called when first errors")
	}
}

func TestChainedDomainHookSecondErrorPropagated(t *testing.T) {
	firstHandler := &appendingDomainHandler{suffix: "-first"}
	errHandler := &errorDomainHandler{err: fmt.Errorf("second handler failed")}

	chained := &chainedDomainHandler{handlers: []LibvirtDomainHookHandler{firstHandler, errHandler}}

	domain := &libvirtxml.Domain{Name: "test"}
	err := chained.MutateDomain(context.Background(), domain, testVMI("test"))

	if err == nil {
		t.Fatal("expected error from second handler")
	}

	if !strings.Contains(err.Error(), "second handler failed") {
		t.Fatalf("expected second handler error, got: %v", err)
	}

	if !firstHandler.called {
		t.Fatal("expected first handler to be called before second errors")
	}

	if domain.Name != "test-first" {
		t.Fatalf("expected domain name 'test-first' after first handler, got %q", domain.Name)
	}
}

func TestServeNonExistentEntrypointErrors(t *testing.T) {
	p := New("test-plugin").WithNodeHook(PreVMStart, NodeHandler(&stubNodeHandler{}))

	err := p.Serve(withEntrypoint("non-existent"))
	if err == nil {
		t.Fatal("expected error for non-existent entrypoint")
	}

	if !strings.Contains(err.Error(), "no hooks registered for entrypoint") {
		t.Fatalf("expected 'no hooks registered for entrypoint' error, got: %v", err)
	}
}
