package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pb "github.com/kubevirt/plugins/api/hooks/v1alpha1"
	"github.com/kubevirt/plugins/pkg/sdk/migration"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	v1 "kubevirt.io/api/core/v1"
	"libvirt.org/go/libvirtxml"
)

type mutatingGuestDefinitionHandler struct {
	receivedVMI               *v1.VirtualMachineInstance
	receivedInvocationContext InvocationContext
}

func (h *mutatingGuestDefinitionHandler) GuestDefinition(_ context.Context, req *GuestDefinitionRequest) error {
	h.receivedVMI = req.VMI
	h.receivedInvocationContext = req.InvocationContext
	req.Domain.Name = "mutated-" + req.Domain.Name
	return nil
}

type panickingGuestDefinitionHandler struct{}

func (h *panickingGuestDefinitionHandler) GuestDefinition(_ context.Context, _ *GuestDefinitionRequest) error {
	panic("handler exploded")
}

type errorGuestDefinitionHandler struct {
	err error
}

func (h *errorGuestDefinitionHandler) GuestDefinition(_ context.Context, _ *GuestDefinitionRequest) error {
	return h.err
}

type recordingPreBootHandler struct {
	receivedVMI           *v1.VirtualMachineInstance
	receivedArtifactPaths map[string]string
}

func (h *recordingPreBootHandler) PreBoot(_ context.Context, req *PreBootRequest) (*PreBootResponse, error) {
	h.receivedVMI = req.VMI
	h.receivedArtifactPaths = req.ArtifactPaths
	return nil, nil
}

type recordingPreMigrationSourceHandler struct {
	receivedRequest *PreMigrationSourceRequest
	response        *PreMigrationSourceResponse
	err             error
}

func (h *recordingPreMigrationSourceHandler) PreMigrationSource(_ context.Context, req *PreMigrationSourceRequest) (*PreMigrationSourceResponse, error) {
	h.receivedRequest = req
	return h.response, h.err
}

type errorNodeHandler struct {
	err error
}

func (h *errorNodeHandler) ExecuteNodeHook(_ context.Context, _ *NodeHookRequest) (*NodeHookResponse, error) {
	return nil, h.err
}

type recordingNodeHandler struct {
	receivedHookPoint NodeHookPoint
	receivedNodeName  string
	receivedVMI       *v1.VirtualMachineInstance
}

func (h *recordingNodeHandler) ExecuteNodeHook(_ context.Context, req *NodeHookRequest) (*NodeHookResponse, error) {
	h.receivedHookPoint = req.HookPoint
	h.receivedNodeName = req.NodeName
	h.receivedVMI = req.VMI
	return nil, nil
}

type panickingNodeHandler struct{}

func (h *panickingNodeHandler) ExecuteNodeHook(_ context.Context, _ *NodeHookRequest) (*NodeHookResponse, error) {
	panic("node handler exploded")
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

func waitForSocketWhileServing(t *testing.T, path string, errCh <-chan error) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-errCh:
			t.Fatalf("serve returned before socket %s became connectable: %v", path, err)
		default:
		}

		conn, err := net.DialTimeout("unix", path, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("socket %s did not become connectable within timeout", path)
}

// unservableSocketPath returns a socket path under a regular file, so serving on it fails.
func unservableSocketPath(t *testing.T) string {
	t.Helper()

	notADirectory := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(notADirectory, nil, 0644); err != nil {
		t.Fatalf("failed to create %s: %v", notADirectory, err)
	}

	return filepath.Join(notADirectory, "x.sock")
}

func serveWithArgs(t *testing.T, plugin *Plugin, args []string, launcherSock, nodeSock string, stopCh <-chan struct{}) <-chan error {
	t.Helper()

	serveOpts, err := parseServeArgs(args)
	if err != nil {
		t.Fatalf("failed to parse serve args %v: %v", args, err)
	}
	serveOpts.launcherSocketPath = launcherSock
	serveOpts.nodeSocketPath = nodeSock
	serveOpts.stopCh = stopCh

	errCh := make(chan error, 1)
	go func() {
		errCh <- plugin.serve(serveOpts)
	}()

	return errCh
}

// --- Adapter unit tests ---

func TestLauncherHookAdapterMutatesDomain(t *testing.T) {
	handler := &mutatingGuestDefinitionHandler{}
	adapter := &launcherHookAdapter{guestDefinitionHandler: handler}

	vmi := testVMI("test-vmi")
	resp, err := adapter.GuestDefinition(context.Background(), &pb.GuestDefinitionRequest{
		DomainType:     "libvirt",
		Domain:         testDomainXML(t, "original"),
		Vmi:            mustMarshalVMI(t, vmi),
		SidecarContext: &pb.SidecarContext{},
	})
	if err != nil {
		t.Fatalf("GuestDefinition failed: %v", err)
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

func TestLauncherHookAdapterPassthroughUnknownDomainType(t *testing.T) {
	handler := &mutatingGuestDefinitionHandler{}
	adapter := &launcherHookAdapter{guestDefinitionHandler: handler}

	originalDomain := []byte("<some-unknown-format/>")
	resp, err := adapter.GuestDefinition(context.Background(), &pb.GuestDefinitionRequest{
		DomainType: "mshv",
		Domain:     originalDomain,
		Vmi:        mustMarshalVMI(t, testVMI("test")),
	})
	if err != nil {
		t.Fatalf("GuestDefinition failed: %v", err)
	}

	if string(resp.Domain) != string(originalDomain) {
		t.Fatalf("expected domain bytes unchanged, got %q", string(resp.Domain))
	}
}

func TestLauncherHookAdapterPassesInvocationContext(t *testing.T) {
	handler := &mutatingGuestDefinitionHandler{}
	adapter := &launcherHookAdapter{guestDefinitionHandler: handler}

	_, err := adapter.GuestDefinition(context.Background(), &pb.GuestDefinitionRequest{
		DomainType:     "libvirt",
		Domain:         testDomainXML(t, "test"),
		Vmi:            mustMarshalVMI(t, testVMI("test")),
		SidecarContext: &pb.SidecarContext{InvocationContext: string(InvocationContextMigrationTarget)},
	})
	if err != nil {
		t.Fatalf("GuestDefinition failed: %v", err)
	}

	if handler.receivedInvocationContext != InvocationContextMigrationTarget {
		t.Fatalf("expected invocation context %q, got %q", InvocationContextMigrationTarget, handler.receivedInvocationContext)
	}
}

func TestLauncherHookAdapterNilSidecarContext(t *testing.T) {
	handler := &mutatingGuestDefinitionHandler{}
	adapter := &launcherHookAdapter{guestDefinitionHandler: handler}

	_, err := adapter.GuestDefinition(context.Background(), &pb.GuestDefinitionRequest{
		DomainType: "libvirt",
		Domain:     testDomainXML(t, "test"),
		Vmi:        mustMarshalVMI(t, testVMI("test")),
	})
	if err != nil {
		t.Fatalf("GuestDefinition failed: %v", err)
	}

	if handler.receivedInvocationContext != "" {
		t.Fatalf("expected empty invocation context, got %q", handler.receivedInvocationContext)
	}
}

func TestLauncherHookAdapterUnimplementedHookPoints(t *testing.T) {
	adapter := &launcherHookAdapter{guestDefinitionHandler: &stubGuestDefinitionHandler{}}

	_, err := adapter.PreBoot(context.Background(), &pb.PreBootRequest{})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("expected Unimplemented from PreBoot, got %v", err)
	}

	_, err = adapter.PreMigrationSource(context.Background(), &pb.PreMigrationSourceRequest{})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("expected Unimplemented from PreMigrationSource, got %v", err)
	}

	emptyAdapter := &launcherHookAdapter{}
	_, err = emptyAdapter.GuestDefinition(context.Background(), &pb.GuestDefinitionRequest{})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("expected Unimplemented from GuestDefinition, got %v", err)
	}
}

func TestLauncherHookAdapterPreBootPassesArtifactPaths(t *testing.T) {
	handler := &recordingPreBootHandler{}
	adapter := &launcherHookAdapter{preBootHandler: handler}

	artifactPaths := map[string]string{
		pb.PreBootArtifactCloudInitISO:     "/var/run/kubevirt-ephemeral-disks/cloud-init-data/default/vmi/noCloud.iso",
		pb.PreBootArtifactEphemeralDiskDir: "/var/run/kubevirt-ephemeral-disks",
	}
	_, err := adapter.PreBoot(context.Background(), &pb.PreBootRequest{
		Vmi:            mustMarshalVMI(t, testVMI("preboot-vmi")),
		PreBootContext: &pb.PreBootContext{ArtifactPaths: artifactPaths},
	})
	if err != nil {
		t.Fatalf("PreBoot failed: %v", err)
	}

	if handler.receivedVMI == nil || handler.receivedVMI.Name != "preboot-vmi" {
		t.Fatal("handler did not receive the correct VMI")
	}

	if handler.receivedArtifactPaths == nil {
		t.Fatal("handler received a nil artifact path map")
	}

	isoPath, ok := handler.receivedArtifactPaths[pb.PreBootArtifactCloudInitISO]
	if !ok || isoPath != artifactPaths[pb.PreBootArtifactCloudInitISO] {
		t.Fatalf("expected cloud-init ISO path to be passed, got %q (%v)", isoPath, ok)
	}
}

func TestLauncherHookAdapterPreBootNilContext(t *testing.T) {
	handler := &recordingPreBootHandler{}
	adapter := &launcherHookAdapter{preBootHandler: handler}

	_, err := adapter.PreBoot(context.Background(), &pb.PreBootRequest{Vmi: mustMarshalVMI(t, testVMI("test"))})
	if err != nil {
		t.Fatalf("PreBoot failed: %v", err)
	}

	if handler.receivedArtifactPaths == nil || len(handler.receivedArtifactPaths) != 0 {
		t.Fatalf("expected a non-nil empty artifact path map, got %v", handler.receivedArtifactPaths)
	}
}

func TestLauncherHookAdapterPreMigrationSourceRequestConversion(t *testing.T) {
	handler := &recordingPreMigrationSourceHandler{}
	adapter := &launcherHookAdapter{preMigrationSourceHandler: handler}

	requestFlags := migration.Live | migration.PeerToPeer
	_, err := adapter.PreMigrationSource(context.Background(), &pb.PreMigrationSourceRequest{
		Vmi: mustMarshalVMI(t, testVMI("migration-vmi")),
		MigrationConfig: &pb.PreMigrationSourceRequest_Libvirt{Libvirt: &pb.LibvirtMigrationConfig{
			Flags:  uint64(requestFlags),
			Params: &pb.LibvirtDomainMigrateParameters{Uri: migration.String("unix://migration.sock"), Bandwidth: migration.Uint64(128)},
		}},
	})
	if err != nil {
		t.Fatalf("PreMigrationSource failed: %v", err)
	}

	req := handler.receivedRequest
	if req.VMI == nil || req.VMI.Name != "migration-vmi" {
		t.Fatal("handler did not receive the correct VMI")
	}

	if !req.Libvirt.Flags.Has(migration.Live) || !req.Libvirt.Flags.Has(migration.PeerToPeer) || req.Libvirt.Flags.Has(migration.Tunnelled) {
		t.Fatalf("expected flags %b, got %b", requestFlags, req.Libvirt.Flags)
	}

	uri, ok := migration.StringValue(req.Libvirt.Params.Uri)
	if !ok || uri != "unix://migration.sock" {
		t.Fatalf("expected uri parameter to be present, got %q (%v)", uri, ok)
	}

	bandwidth, ok := migration.Uint64Value(req.Libvirt.Params.Bandwidth)
	if !ok || bandwidth != 128 {
		t.Fatalf("expected bandwidth parameter to be present, got %d (%v)", bandwidth, ok)
	}

	if _, ok := migration.StringValue(req.Libvirt.Params.DestName); ok {
		t.Fatal("expected absent dest_name parameter")
	}
}

func TestLauncherHookAdapterPreMigrationSourceNilParams(t *testing.T) {
	handler := &recordingPreMigrationSourceHandler{}
	adapter := &launcherHookAdapter{preMigrationSourceHandler: handler}

	_, err := adapter.PreMigrationSource(context.Background(), &pb.PreMigrationSourceRequest{
		Vmi:             mustMarshalVMI(t, testVMI("test")),
		MigrationConfig: &pb.PreMigrationSourceRequest_Libvirt{Libvirt: &pb.LibvirtMigrationConfig{Flags: uint64(migration.Live)}},
	})
	if err != nil {
		t.Fatalf("PreMigrationSource failed: %v", err)
	}

	req := handler.receivedRequest
	if req.Libvirt.Params == nil {
		t.Fatal("expected nil params to be replaced by an empty message")
	}

	if _, ok := migration.StringValue(req.Libvirt.Params.Uri); ok {
		t.Fatal("expected no uri parameter in the empty message")
	}
}

func TestLauncherHookAdapterPreMigrationSourceNilMigrationConfig(t *testing.T) {
	handler := &recordingPreMigrationSourceHandler{}
	adapter := &launcherHookAdapter{preMigrationSourceHandler: handler}

	_, err := adapter.PreMigrationSource(context.Background(), &pb.PreMigrationSourceRequest{
		Vmi: mustMarshalVMI(t, testVMI("test")),
	})
	if err != nil {
		t.Fatalf("PreMigrationSource failed: %v", err)
	}

	if handler.receivedRequest.Libvirt != nil {
		t.Fatalf("expected a nil Libvirt config to reach the handler, got %+v", handler.receivedRequest.Libvirt)
	}
}

func TestLauncherHookAdapterPreMigrationSourceResponseConversion(t *testing.T) {
	handler := &recordingPreMigrationSourceHandler{
		response: &PreMigrationSourceResponse{Libvirt: &LibvirtMigrationChanges{
			SetFlags:   migration.Parallel,
			ClearFlags: migration.Compressed,
			Params:     &LibvirtDomainMigrateParameters{Uri: migration.String("unix://overridden.sock"), MigrateDisks: migration.Strings("vda", "vdb")},
		}},
	}
	adapter := &launcherHookAdapter{preMigrationSourceHandler: handler}

	resp, err := adapter.PreMigrationSource(context.Background(), &pb.PreMigrationSourceRequest{
		Vmi:             mustMarshalVMI(t, testVMI("test")),
		MigrationConfig: &pb.PreMigrationSourceRequest_Libvirt{Libvirt: &pb.LibvirtMigrationConfig{}},
	})
	if err != nil {
		t.Fatalf("PreMigrationSource failed: %v", err)
	}

	changes := resp.GetLibvirt()
	if changes == nil {
		t.Fatal("expected the libvirt response variant")
	}

	if changes.GetSetFlags() != uint64(migration.Parallel) || changes.GetClearFlags() != uint64(migration.Compressed) {
		t.Fatalf("expected set/clear flags, got %d/%d", changes.GetSetFlags(), changes.GetClearFlags())
	}

	uri, ok := migration.StringValue(changes.GetParams().GetUri())
	if !ok || uri != "unix://overridden.sock" {
		t.Fatalf("expected uri override on the wire, got %q (%v)", uri, ok)
	}

	disks, ok := migration.StringsValue(changes.GetParams().GetMigrateDisks())
	if !ok || len(disks) != 2 || disks[0] != "vda" || disks[1] != "vdb" {
		t.Fatalf("expected migrate_disks override on the wire, got %v (%v)", disks, ok)
	}
}

func TestLauncherHookAdapterPreMigrationSourceRemoveParams(t *testing.T) {
	handler := &recordingPreMigrationSourceHandler{
		response: &PreMigrationSourceResponse{Libvirt: &LibvirtMigrationChanges{
			Params: &LibvirtDomainMigrateParameters{Uri: migration.RemoveString(), MigrateDisks: migration.RemoveStrings()},
		}},
	}
	adapter := &launcherHookAdapter{preMigrationSourceHandler: handler}

	resp, err := adapter.PreMigrationSource(context.Background(), &pb.PreMigrationSourceRequest{
		Vmi:             mustMarshalVMI(t, testVMI("test")),
		MigrationConfig: &pb.PreMigrationSourceRequest_Libvirt{Libvirt: &pb.LibvirtMigrationConfig{}},
	})
	if err != nil {
		t.Fatalf("PreMigrationSource failed: %v", err)
	}

	changes := resp.GetLibvirt()
	if changes == nil {
		t.Fatal("expected the libvirt response variant")
	}

	if changes.GetParams().GetUri() == nil || changes.GetParams().GetUri().GetKind() != nil {
		t.Fatal("expected an empty wrapper on the wire for a removed string parameter")
	}

	if changes.GetParams().GetMigrateDisks() == nil || len(changes.GetParams().GetMigrateDisks().GetValues()) != 0 {
		t.Fatal("expected an empty list on the wire for a removed string list parameter")
	}
}

func TestLauncherHookAdapterPreMigrationSourceNilResponse(t *testing.T) {
	handler := &recordingPreMigrationSourceHandler{}
	adapter := &launcherHookAdapter{preMigrationSourceHandler: handler}

	resp, err := adapter.PreMigrationSource(context.Background(), &pb.PreMigrationSourceRequest{
		Vmi:             mustMarshalVMI(t, testVMI("test")),
		MigrationConfig: &pb.PreMigrationSourceRequest_Libvirt{Libvirt: &pb.LibvirtMigrationConfig{}},
	})
	if err != nil {
		t.Fatalf("PreMigrationSource failed: %v", err)
	}

	if resp.GetLibvirt() != nil {
		t.Fatalf("expected an empty response, got %+v", resp)
	}

	handler.response = &PreMigrationSourceResponse{}
	resp, err = adapter.PreMigrationSource(context.Background(), &pb.PreMigrationSourceRequest{
		Vmi:             mustMarshalVMI(t, testVMI("test")),
		MigrationConfig: &pb.PreMigrationSourceRequest_Libvirt{Libvirt: &pb.LibvirtMigrationConfig{}},
	})
	if err != nil {
		t.Fatalf("PreMigrationSource failed: %v", err)
	}

	if resp.GetLibvirt() != nil {
		t.Fatalf("expected an empty response for a nil Libvirt, got %+v", resp)
	}
}

func TestLauncherHookAdapterPreMigrationSourceOverlappingFlagsError(t *testing.T) {
	handler := &recordingPreMigrationSourceHandler{
		response: &PreMigrationSourceResponse{Libvirt: &LibvirtMigrationChanges{
			SetFlags:   migration.Live | migration.Unsafe,
			ClearFlags: migration.Live | migration.PeerToPeer,
		}},
	}
	adapter := &launcherHookAdapter{preMigrationSourceHandler: handler}

	_, err := adapter.PreMigrationSource(context.Background(), &pb.PreMigrationSourceRequest{
		Vmi:             mustMarshalVMI(t, testVMI("test")),
		MigrationConfig: &pb.PreMigrationSourceRequest_Libvirt{Libvirt: &pb.LibvirtMigrationConfig{}},
	})
	if err == nil {
		t.Fatal("expected an error for overlapping set and clear flags")
	}

	if !strings.Contains(err.Error(), "both set and cleared") || !strings.Contains(err.Error(), "0b1") {
		t.Fatalf("expected the error to name the overlapping bits, got: %v", err)
	}
}

func TestLauncherHookAdapterPreMigrationSourceHandlerError(t *testing.T) {
	handler := &recordingPreMigrationSourceHandler{err: fmt.Errorf("handler broke")}
	adapter := &launcherHookAdapter{preMigrationSourceHandler: handler}

	_, err := adapter.PreMigrationSource(context.Background(), &pb.PreMigrationSourceRequest{
		Vmi:             mustMarshalVMI(t, testVMI("test")),
		MigrationConfig: &pb.PreMigrationSourceRequest_Libvirt{Libvirt: &pb.LibvirtMigrationConfig{}},
	})
	if err == nil || !strings.Contains(err.Error(), "handler broke") {
		t.Fatalf("expected handler error to be propagated, got: %v", err)
	}
}

func TestNodeHookAdapterCallsHandler(t *testing.T) {
	handler := &recordingNodeHandler{}
	adapter := &nodeHookAdapter{handlers: map[NodeHookPoint]NodeHookHandler{NodeHookPreVMStart: handler}}

	vmi := testVMI("node-vmi")
	_, err := adapter.ExecuteNodeHook(context.Background(), &pb.ExecuteNodeHookRequest{
		HookPoint:   string(NodeHookPreVMStart),
		Vmi:         mustMarshalVMI(t, vmi),
		NodeContext: &pb.NodeContext{NodeName: "worker-1"},
	})
	if err != nil {
		t.Fatalf("ExecuteNodeHook failed: %v", err)
	}

	if handler.receivedHookPoint != NodeHookPreVMStart {
		t.Fatalf("expected hook point %q, got %q", NodeHookPreVMStart, handler.receivedHookPoint)
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
	adapter := &nodeHookAdapter{handlers: map[NodeHookPoint]NodeHookHandler{NodeHookPreVMStart: handler}}

	_, err := adapter.ExecuteNodeHook(context.Background(), &pb.ExecuteNodeHookRequest{
		HookPoint: string(NodeHookPreVMStart),
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
	launcherSock := filepath.Join(tmpDir, "launcher.sock")

	handler := &mutatingGuestDefinitionHandler{}
	p := New("test-plugin").WithLauncherHook(GuestDefinition(handler))

	stopCh := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- p.serve(serveOptions{launcherSocketPath: launcherSock, stopCh: stopCh})
	}()

	waitForSocket(t, launcherSock)

	close(stopCh)
	awaitServeStop(t, errCh)
}

func TestServeRegistersLauncherHookAdapter(t *testing.T) {
	tmpDir := t.TempDir()
	launcherSock := filepath.Join(tmpDir, "launcher.sock")

	handler := &mutatingGuestDefinitionHandler{}
	p := New("test-plugin").WithLauncherHook(GuestDefinition(handler))

	stopCh := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- p.serve(serveOptions{launcherSocketPath: launcherSock, stopCh: stopCh})
	}()

	waitForSocket(t, launcherSock)

	conn := dialSocket(t, launcherSock)
	client := pb.NewLauncherHookServiceClient(conn)

	resp, err := client.GuestDefinition(context.Background(), &pb.GuestDefinitionRequest{
		DomainType:     "libvirt",
		Domain:         testDomainXML(t, "original"),
		Vmi:            mustMarshalVMI(t, testVMI("grpc-test-vmi")),
		SidecarContext: &pb.SidecarContext{},
	})
	if err != nil {
		t.Fatalf("GuestDefinition RPC failed: %v", err)
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

func TestServeServesAllLauncherHookPointsOfAnEntrypoint(t *testing.T) {
	tmpDir := t.TempDir()
	launcherSock := filepath.Join(tmpDir, "launcher.sock")

	guestDefinitionHandler := &mutatingGuestDefinitionHandler{}
	preBootHandler := &recordingPreBootHandler{}
	preMigrationSourceHandler := &recordingPreMigrationSourceHandler{}
	p := New("test-plugin").
		WithLauncherHook(GuestDefinition(guestDefinitionHandler)).
		WithLauncherHook(PreBoot(preBootHandler)).
		WithLauncherHook(PreMigrationSource(preMigrationSourceHandler))

	stopCh := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- p.serve(serveOptions{launcherSocketPath: launcherSock, stopCh: stopCh})
	}()

	waitForSocket(t, launcherSock)

	conn := dialSocket(t, launcherSock)
	client := pb.NewLauncherHookServiceClient(conn)

	if _, err := client.GuestDefinition(context.Background(), &pb.GuestDefinitionRequest{DomainType: "libvirt", Domain: testDomainXML(t, "original"), Vmi: mustMarshalVMI(t, testVMI("test")), SidecarContext: &pb.SidecarContext{}}); err != nil {
		t.Fatalf("GuestDefinition RPC failed: %v", err)
	}

	if _, err := client.PreBoot(context.Background(), &pb.PreBootRequest{Vmi: mustMarshalVMI(t, testVMI("test")), PreBootContext: &pb.PreBootContext{ArtifactPaths: map[string]string{pb.PreBootArtifactEphemeralDiskDir: "/var/run/kubevirt-ephemeral-disks"}}}); err != nil {
		t.Fatalf("PreBoot RPC failed: %v", err)
	}

	if _, err := client.PreMigrationSource(context.Background(), &pb.PreMigrationSourceRequest{Vmi: mustMarshalVMI(t, testVMI("test")), MigrationConfig: &pb.PreMigrationSourceRequest_Libvirt{Libvirt: &pb.LibvirtMigrationConfig{}}}); err != nil {
		t.Fatalf("PreMigrationSource RPC failed: %v", err)
	}

	if guestDefinitionHandler.receivedVMI == nil {
		t.Fatal("expected the GuestDefinition handler to be called")
	}

	if preBootHandler.receivedVMI == nil {
		t.Fatal("expected the PreBoot handler to be called")
	}

	if preMigrationSourceHandler.receivedRequest == nil {
		t.Fatal("expected the PreMigrationSource handler to be called")
	}

	close(stopCh)
	awaitServeStop(t, errCh)
}

func TestServePanicReturnsInternal(t *testing.T) {
	tmpDir := t.TempDir()
	launcherSock := filepath.Join(tmpDir, "launcher.sock")

	p := New("test-plugin").WithLauncherHook(GuestDefinition(&panickingGuestDefinitionHandler{}))

	stopCh := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- p.serve(serveOptions{launcherSocketPath: launcherSock, stopCh: stopCh})
	}()

	waitForSocket(t, launcherSock)

	conn := dialSocket(t, launcherSock)
	client := pb.NewLauncherHookServiceClient(conn)

	_, err := client.GuestDefinition(context.Background(), &pb.GuestDefinitionRequest{
		DomainType:     "libvirt",
		Domain:         testDomainXML(t, "original"),
		Vmi:            mustMarshalVMI(t, testVMI("test")),
		SidecarContext: &pb.SidecarContext{},
	})
	if status.Code(err) != codes.Internal {
		t.Fatalf("expected Internal from a panicking handler, got %v", err)
	}

	// The server must survive the panic and keep serving.
	resp, err := client.GuestDefinition(context.Background(), &pb.GuestDefinitionRequest{
		DomainType: "mshv",
		Domain:     []byte("<passthrough/>"),
		Vmi:        mustMarshalVMI(t, testVMI("test")),
	})
	if err != nil {
		t.Fatalf("GuestDefinition RPC after panic failed: %v", err)
	}

	if string(resp.Domain) != "<passthrough/>" {
		t.Fatalf("expected passthrough after panic, got %q", string(resp.Domain))
	}

	close(stopCh)
	awaitServeStop(t, errCh)
}

func TestServeNodeHookPanicReturnsInternal(t *testing.T) {
	tmpDir := t.TempDir()
	nodeSock := filepath.Join(tmpDir, "node.sock")

	p := New("test-plugin").WithNodeHook(NodeHookPreVMStart, NodeHandler(&panickingNodeHandler{}))

	stopCh := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- p.serve(serveOptions{nodeSocketPath: nodeSock, stopCh: stopCh})
	}()

	waitForSocket(t, nodeSock)

	conn := dialSocket(t, nodeSock)
	client := pb.NewNodeHookServiceClient(conn)

	_, err := client.ExecuteNodeHook(context.Background(), &pb.ExecuteNodeHookRequest{
		HookPoint:   string(NodeHookPreVMStart),
		Vmi:         mustMarshalVMI(t, testVMI("test")),
		NodeContext: &pb.NodeContext{NodeName: "node-1"},
	})
	if status.Code(err) != codes.Internal {
		t.Fatalf("expected Internal from a panicking node handler, got %v", err)
	}

	// The server must survive the panic and keep serving.
	_, err = client.ExecuteNodeHook(context.Background(), &pb.ExecuteNodeHookRequest{
		HookPoint:   string(NodeHookPreVMStart),
		Vmi:         mustMarshalVMI(t, testVMI("test")),
		NodeContext: &pb.NodeContext{NodeName: "node-1"},
	})
	if status.Code(err) != codes.Internal {
		t.Fatalf("expected the server to survive the panic, got %v", err)
	}

	close(stopCh)
	awaitServeStop(t, errCh)
}

func TestServeRegistersNodeHookAdapter(t *testing.T) {
	tmpDir := t.TempDir()
	nodeSock := filepath.Join(tmpDir, "node.sock")

	handler := &recordingNodeHandler{}
	p := New("test-plugin").WithNodeHook(NodeHookPreVMStart, NodeHandler(handler))

	stopCh := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- p.serve(serveOptions{nodeSocketPath: nodeSock, stopCh: stopCh})
	}()

	waitForSocket(t, nodeSock)

	conn := dialSocket(t, nodeSock)
	client := pb.NewNodeHookServiceClient(conn)

	_, err := client.ExecuteNodeHook(context.Background(), &pb.ExecuteNodeHookRequest{
		HookPoint:   string(NodeHookPreVMStart),
		Vmi:         mustMarshalVMI(t, testVMI("node-test-vmi")),
		NodeContext: &pb.NodeContext{NodeName: "test-node"},
	})
	if err != nil {
		t.Fatalf("ExecuteNodeHook RPC failed: %v", err)
	}

	if handler.receivedHookPoint != NodeHookPreVMStart {
		t.Fatalf("expected hook point %q, got %q", NodeHookPreVMStart, handler.receivedHookPoint)
	}

	if handler.receivedNodeName != "test-node" {
		t.Fatalf("expected node name %q, got %q", "test-node", handler.receivedNodeName)
	}

	close(stopCh)
	awaitServeStop(t, errCh)
}

// --- Adapter error-path tests ---

func TestLauncherHookAdapterInvalidXML(t *testing.T) {
	adapter := &launcherHookAdapter{guestDefinitionHandler: &mutatingGuestDefinitionHandler{}}

	_, err := adapter.GuestDefinition(context.Background(), &pb.GuestDefinitionRequest{
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

func TestLauncherHookAdapterInvalidVMIJSON(t *testing.T) {
	adapter := &launcherHookAdapter{guestDefinitionHandler: &mutatingGuestDefinitionHandler{}}

	_, err := adapter.GuestDefinition(context.Background(), &pb.GuestDefinitionRequest{
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

func TestLauncherHookAdapterPreBootInvalidVMIJSON(t *testing.T) {
	handler := &recordingPreBootHandler{}
	adapter := &launcherHookAdapter{preBootHandler: handler}

	_, err := adapter.PreBoot(context.Background(), &pb.PreBootRequest{Vmi: []byte("not-valid-json{{{")})
	if err == nil {
		t.Fatal("expected error for invalid VMI JSON")
	}

	if !strings.Contains(err.Error(), "unmarshal VMI") {
		t.Fatalf("expected VMI unmarshal error, got: %v", err)
	}
}

func TestLauncherHookAdapterPreMigrationSourceInvalidVMIJSON(t *testing.T) {
	handler := &recordingPreMigrationSourceHandler{}
	adapter := &launcherHookAdapter{preMigrationSourceHandler: handler}

	_, err := adapter.PreMigrationSource(context.Background(), &pb.PreMigrationSourceRequest{Vmi: []byte("not-valid-json{{{")})
	if err == nil {
		t.Fatal("expected error for invalid VMI JSON")
	}

	if !strings.Contains(err.Error(), "unmarshal VMI") {
		t.Fatalf("expected VMI unmarshal error, got: %v", err)
	}
}

func TestLauncherHookAdapterHandlerError(t *testing.T) {
	handlerErr := fmt.Errorf("handler broke")
	adapter := &launcherHookAdapter{guestDefinitionHandler: &errorGuestDefinitionHandler{err: handlerErr}}

	_, err := adapter.GuestDefinition(context.Background(), &pb.GuestDefinitionRequest{
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
	adapter := &nodeHookAdapter{handlers: map[NodeHookPoint]NodeHookHandler{
		NodeHookPreVMStart: &recordingNodeHandler{},
	}}

	_, err := adapter.ExecuteNodeHook(context.Background(), &pb.ExecuteNodeHookRequest{
		HookPoint:   "NonExistentHookPoint",
		Vmi:         mustMarshalVMI(t, testVMI("test")),
		NodeContext: &pb.NodeContext{NodeName: "node-1"},
	})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("expected Unimplemented for unregistered hook point, got %v", err)
	}
}

func TestNodeHookAdapterInvalidVMIJSON(t *testing.T) {
	adapter := &nodeHookAdapter{handlers: map[NodeHookPoint]NodeHookHandler{
		NodeHookPreVMStart: &recordingNodeHandler{},
	}}

	_, err := adapter.ExecuteNodeHook(context.Background(), &pb.ExecuteNodeHookRequest{
		HookPoint:   string(NodeHookPreVMStart),
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
	adapter := &nodeHookAdapter{handlers: map[NodeHookPoint]NodeHookHandler{
		NodeHookPreVMStart: &errorNodeHandler{err: handlerErr},
	}}

	_, err := adapter.ExecuteNodeHook(context.Background(), &pb.ExecuteNodeHookRequest{
		HookPoint:   string(NodeHookPreVMStart),
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

type appendingGuestDefinitionHandler struct {
	suffix string
	called bool
}

func (h *appendingGuestDefinitionHandler) GuestDefinition(_ context.Context, req *GuestDefinitionRequest) error {
	h.called = true
	req.Domain.Name = req.Domain.Name + h.suffix
	return nil
}

func TestServeWithEntrypointFiltersLauncherHooks(t *testing.T) {
	tmpDir := t.TempDir()
	launcherSock := filepath.Join(tmpDir, "launcher.sock")

	handlerA := &appendingGuestDefinitionHandler{suffix: "-A"}
	handlerB := &appendingGuestDefinitionHandler{suffix: "-B"}

	p := New("test-plugin").
		WithLauncherHook(GuestDefinition(handlerA).WithEntrypoint("ep-a")).
		WithLauncherHook(GuestDefinition(handlerB).WithEntrypoint("ep-b"))

	stopCh := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- p.serve(serveOptions{launcherSocketPath: launcherSock, stopCh: stopCh, entrypoint: "ep-a"})
	}()

	waitForSocket(t, launcherSock)

	conn := dialSocket(t, launcherSock)
	client := pb.NewLauncherHookServiceClient(conn)

	resp, err := client.GuestDefinition(context.Background(), &pb.GuestDefinitionRequest{
		DomainType:     "libvirt",
		Domain:         testDomainXML(t, "original"),
		Vmi:            mustMarshalVMI(t, testVMI("test")),
		SidecarContext: &pb.SidecarContext{},
	})
	if err != nil {
		t.Fatalf("GuestDefinition RPC failed: %v", err)
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
		WithNodeHook(NodeHookPreVMStart, NodeHandler(handlerA).WithEntrypoint("ep-a")).
		WithNodeHook(NodeHookPostVMStop, NodeHandler(handlerB).WithEntrypoint("ep-b"))

	stopCh := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- p.serve(serveOptions{nodeSocketPath: nodeSock, stopCh: stopCh, entrypoint: "ep-a"})
	}()

	waitForSocket(t, nodeSock)

	conn := dialSocket(t, nodeSock)
	client := pb.NewNodeHookServiceClient(conn)

	_, err := client.ExecuteNodeHook(context.Background(), &pb.ExecuteNodeHookRequest{
		HookPoint:   string(NodeHookPreVMStart),
		Vmi:         mustMarshalVMI(t, testVMI("test")),
		NodeContext: &pb.NodeContext{NodeName: "worker-1"},
	})
	if err != nil {
		t.Fatalf("ExecuteNodeHook RPC failed: %v", err)
	}

	if handlerA.receivedHookPoint != NodeHookPreVMStart {
		t.Fatalf("expected handler A to receive %q, got %q", NodeHookPreVMStart, handlerA.receivedHookPoint)
	}

	if handlerB.receivedHookPoint != "" {
		t.Fatal("expected handler B to NOT be called")
	}

	close(stopCh)
	awaitServeStop(t, errCh)
}

func TestServeMixedEntrypointsWithoutFlagErrors(t *testing.T) {
	p := New("test-plugin").
		WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{}).WithEntrypoint("ep-a")).
		WithNodeHook(NodeHookPostVMStop, NodeHandler(&stubNodeHandler{}).WithEntrypoint("ep-b"))

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
	launcherSock := filepath.Join(tmpDir, "launcher.sock")

	p := New("test-plugin").WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{}))

	stopCh := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- p.serve(serveOptions{launcherSocketPath: launcherSock, stopCh: stopCh})
	}()

	waitForSocket(t, launcherSock)

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

func TestServeNonExistentEntrypointErrors(t *testing.T) {
	p := New("test-plugin").WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{}))

	err := p.serve(serveOptions{entrypoint: "non-existent"})
	if err == nil {
		t.Fatal("expected error for non-existent entrypoint")
	}

	if !strings.Contains(err.Error(), "no hooks registered for entrypoint") {
		t.Fatalf("expected 'no hooks registered for entrypoint' error, got: %v", err)
	}
}

func TestServeCELOnlyPluginErrors(t *testing.T) {
	p := New("test-plugin").WithLauncherHook(GuestDefinitionCEL("Domain{Name: 'test'}"))

	err := p.Serve()
	if err == nil {
		t.Fatal("expected error when serving CEL-only plugin")
	}

	if !strings.Contains(err.Error(), "CEL launcher hooks") {
		t.Fatalf("expected CEL-specific error message, got: %v", err)
	}
}

func TestServeMixedSidecarAndCEL(t *testing.T) {
	tmpDir := t.TempDir()
	launcherSock := filepath.Join(tmpDir, "launcher.sock")

	handler := &mutatingGuestDefinitionHandler{}
	p := New("test-plugin").
		WithLauncherHook(GuestDefinition(handler)).
		WithLauncherHook(GuestDefinitionCEL("Domain{Name: 'test'}"))

	stopCh := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- p.serve(serveOptions{launcherSocketPath: launcherSock, stopCh: stopCh})
	}()

	waitForSocket(t, launcherSock)

	conn := dialSocket(t, launcherSock)
	client := pb.NewLauncherHookServiceClient(conn)

	resp, err := client.GuestDefinition(context.Background(), &pb.GuestDefinitionRequest{
		DomainType:     "libvirt",
		Domain:         testDomainXML(t, "original"),
		Vmi:            mustMarshalVMI(t, testVMI("test")),
		SidecarContext: &pb.SidecarContext{},
	})
	if err != nil {
		t.Fatalf("GuestDefinition RPC failed: %v", err)
	}

	var result libvirtxml.Domain
	if err := result.Unmarshal(string(resp.Domain)); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if result.Name != "mutated-original" {
		t.Fatalf("expected domain name 'mutated-original', got %q", result.Name)
	}

	close(stopCh)
	awaitServeStop(t, errCh)
}

func TestServeWithSidecarArgsServesOnlyLauncherHooks(t *testing.T) {
	tests := []struct {
		name       string
		entrypoint string
	}{
		{name: "default entrypoint"},
		{name: "named entrypoint", entrypoint: "ep-a"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			launcherSock := filepath.Join(t.TempDir(), "launcher.sock")
			nodeSock := unservableSocketPath(t)

			plugin := New("test-plugin").
				WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{}).WithEntrypoint(test.entrypoint)).
				WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{}).WithEntrypoint(test.entrypoint))

			stopCh := make(chan struct{})
			args := []string{"--entrypoint", plugin.resolveEntrypoint(test.entrypoint), "--role", roleSidecar}
			errCh := serveWithArgs(t, plugin, args, launcherSock, nodeSock, stopCh)

			waitForSocketWhileServing(t, launcherSock, errCh)

			close(stopCh)
			awaitServeStop(t, errCh)
		})
	}
}

func TestServeWithDaemonSetArgsServesOnlyNodeHooks(t *testing.T) {
	tests := []struct {
		name       string
		entrypoint string
	}{
		{name: "default entrypoint"},
		{name: "named entrypoint", entrypoint: "ep-a"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			launcherSock := unservableSocketPath(t)
			nodeSock := filepath.Join(t.TempDir(), "node.sock")

			plugin := New("test-plugin").
				WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{}).WithEntrypoint(test.entrypoint)).
				WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{}).WithEntrypoint(test.entrypoint))

			stopCh := make(chan struct{})
			args := []string{"--entrypoint", plugin.resolveEntrypoint(test.entrypoint), "--role", roleDaemonSet}
			errCh := serveWithArgs(t, plugin, args, launcherSock, nodeSock, stopCh)

			waitForSocketWhileServing(t, nodeSock, errCh)

			close(stopCh)
			awaitServeStop(t, errCh)
		})
	}
}

func TestServeWithDefaultEntrypointArgsAlongsideNamedEntrypoint(t *testing.T) {
	plugin := New("test-plugin").
		WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{})).
		WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{})).
		WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{}).WithEntrypoint("ep-a"))

	t.Run("sidecar", func(t *testing.T) {
		launcherSock := filepath.Join(t.TempDir(), "launcher.sock")
		nodeSock := filepath.Join(t.TempDir(), "node.sock")

		stopCh := make(chan struct{})
		errCh := serveWithArgs(t, plugin, []string{"--entrypoint", "test-plugin", "--role", roleSidecar}, launcherSock, nodeSock, stopCh)

		waitForSocketWhileServing(t, launcherSock, errCh)

		close(stopCh)
		awaitServeStop(t, errCh)
	})

	t.Run("daemonset", func(t *testing.T) {
		launcherSock := filepath.Join(t.TempDir(), "launcher.sock")
		nodeSock := filepath.Join(t.TempDir(), "node.sock")

		stopCh := make(chan struct{})
		errCh := serveWithArgs(t, plugin, []string{"--entrypoint", "test-plugin", "--role", roleDaemonSet}, launcherSock, nodeSock, stopCh)

		waitForSocketWhileServing(t, nodeSock, errCh)

		close(stopCh)
		awaitServeStop(t, errCh)
	})
}

func TestParseServeArgs(t *testing.T) {
	serveOpts, err := parseServeArgs([]string{"--entrypoint", "ep-a", "--role", roleSidecar})
	if err != nil {
		t.Fatalf("parseServeArgs failed: %v", err)
	}

	if serveOpts.entrypoint != "ep-a" {
		t.Fatalf("expected entrypoint %q, got %q", "ep-a", serveOpts.entrypoint)
	}

	if serveOpts.role != roleSidecar {
		t.Fatalf("expected role %q, got %q", roleSidecar, serveOpts.role)
	}
}

func TestParseServeArgsRejectsInvalidArgs(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		expectedError string
	}{
		{name: "unknown role", args: []string{"--role", "controller"}, expectedError: "unknown role"},
		{name: "unexpected argument", args: []string{"--entrypoint", "ep-a", "sidecar"}, expectedError: "unexpected arguments"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseServeArgs(test.args)
			if err == nil {
				t.Fatalf("expected error for args %v", test.args)
			}

			if !strings.Contains(err.Error(), test.expectedError) {
				t.Fatalf("expected %q error, got: %v", test.expectedError, err)
			}
		})
	}
}

func TestParseServeArgsHelp(t *testing.T) {
	_, err := parseServeArgs([]string{"--help"})
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("expected flag.ErrHelp, got %v", err)
	}
}

func TestServeWithRoleWithoutMatchingHooksErrors(t *testing.T) {
	tests := []struct {
		name          string
		plugin        *Plugin
		role          string
		expectedError string
	}{
		{name: "sidecar without launcher hooks", plugin: New("test-plugin").WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{})), role: roleSidecar, expectedError: "no launcher hooks registered"},
		{name: "daemonset without node hooks", plugin: New("test-plugin").WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{})), role: roleDaemonSet, expectedError: "no node hooks registered"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.plugin.serve(serveOptions{role: test.role})
			if err == nil {
				t.Fatalf("expected error when serving role %q without matching hooks", test.role)
			}

			if !strings.Contains(err.Error(), test.expectedError) {
				t.Fatalf("expected %q error, got: %v", test.expectedError, err)
			}
		})
	}
}

func TestServeBothHookKindsWithoutRoleErrors(t *testing.T) {
	plugin := New("test-plugin").
		WithLauncherHook(GuestDefinition(&stubGuestDefinitionHandler{})).
		WithNodeHook(NodeHookPreVMStart, NodeHandler(&stubNodeHandler{}))

	err := plugin.Serve()
	if err == nil {
		t.Fatal("expected error when serving launcher and node hooks without a role")
	}

	if !strings.Contains(err.Error(), "use --role") {
		t.Fatalf("expected error to suggest --role, got: %v", err)
	}
}
