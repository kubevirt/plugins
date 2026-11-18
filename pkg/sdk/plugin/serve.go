package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"
	"time"

	pb "github.com/kubevirt/plugins/api/hooks/v1alpha1"
	"github.com/kubevirt/plugins/pkg/sdk/migration"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	v1 "kubevirt.io/api/core/v1"
	"libvirt.org/go/libvirtxml"
)

type launcherHookAdapter struct {
	guestDefinitionHandler    GuestDefinitionHandler
	preBootHandler            PreBootHandler
	preMigrationSourceHandler PreMigrationSourceHandler
}

func (a *launcherHookAdapter) GuestDefinition(ctx context.Context, req *pb.GuestDefinitionRequest) (*pb.GuestDefinitionResponse, error) {
	if a.guestDefinitionHandler == nil {
		return nil, status.Errorf(codes.Unimplemented, "GuestDefinition is not implemented by this plugin")
	}

	if req.GetDomainType() != domainTypeLibvirt {
		return &pb.GuestDefinitionResponse{Domain: req.GetDomain()}, nil
	}

	var domain libvirtxml.Domain
	if err := domain.Unmarshal(string(req.GetDomain())); err != nil {
		return nil, fmt.Errorf("failed to unmarshal domain XML: %w", err)
	}

	var vmi v1.VirtualMachineInstance
	if err := json.Unmarshal(req.GetVmi(), &vmi); err != nil {
		return nil, fmt.Errorf("failed to unmarshal VMI: %w", err)
	}

	if err := a.guestDefinitionHandler.GuestDefinition(ctx, &GuestDefinitionRequest{Domain: &domain, VMI: &vmi, InvocationContext: InvocationContext(req.GetSidecarContext().GetInvocationContext())}); err != nil {
		return nil, fmt.Errorf("guest definition handler failed: %w", err)
	}

	xmlStr, err := domain.Marshal()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal domain XML: %w", err)
	}

	return &pb.GuestDefinitionResponse{Domain: []byte(xmlStr)}, nil
}

func (a *launcherHookAdapter) PreBoot(ctx context.Context, req *pb.PreBootRequest) (*pb.PreBootResponse, error) {
	if a.preBootHandler == nil {
		return nil, status.Errorf(codes.Unimplemented, "PreBoot is not implemented by this plugin")
	}

	var vmi v1.VirtualMachineInstance
	if err := json.Unmarshal(req.GetVmi(), &vmi); err != nil {
		return nil, fmt.Errorf("failed to unmarshal VMI: %w", err)
	}

	artifactPaths := make(map[string]string, len(req.GetPreBootContext().GetArtifactPaths()))
	for key, path := range req.GetPreBootContext().GetArtifactPaths() {
		artifactPaths[key] = path
	}

	if _, err := a.preBootHandler.PreBoot(ctx, &PreBootRequest{VMI: &vmi, ArtifactPaths: artifactPaths}); err != nil {
		return nil, fmt.Errorf("pre boot handler failed: %w", err)
	}

	return &pb.PreBootResponse{}, nil
}

func (a *launcherHookAdapter) PreMigrationSource(ctx context.Context, req *pb.PreMigrationSourceRequest) (*pb.PreMigrationSourceResponse, error) {
	if a.preMigrationSourceHandler == nil {
		return nil, status.Errorf(codes.Unimplemented, "PreMigrationSource is not implemented by this plugin")
	}

	var vmi v1.VirtualMachineInstance
	if err := json.Unmarshal(req.GetVmi(), &vmi); err != nil {
		return nil, fmt.Errorf("failed to unmarshal VMI: %w", err)
	}

	var libvirtConfig *LibvirtMigrationConfig
	if req.GetLibvirt() != nil {
		libvirtConfig = &LibvirtMigrationConfig{Flags: migration.Flags(req.GetLibvirt().GetFlags()), Params: req.GetLibvirt().GetParams()}
		if libvirtConfig.Params == nil {
			libvirtConfig.Params = &LibvirtDomainMigrateParameters{}
		}
	}

	resp, err := a.preMigrationSourceHandler.PreMigrationSource(ctx, &PreMigrationSourceRequest{VMI: &vmi, Libvirt: libvirtConfig})
	if err != nil {
		return nil, fmt.Errorf("pre migration source handler failed: %w", err)
	}
	if resp == nil || resp.Libvirt == nil {
		return &pb.PreMigrationSourceResponse{}, nil
	}

	overlap := resp.Libvirt.SetFlags & resp.Libvirt.ClearFlags
	if overlap != 0 {
		return nil, fmt.Errorf("invalid migration flag change: bits 0b%b are both set and cleared", overlap)
	}

	return &pb.PreMigrationSourceResponse{
		MigrationConfig: &pb.PreMigrationSourceResponse_Libvirt{
			Libvirt: &pb.LibvirtMigrationConfigChanges{SetFlags: uint64(resp.Libvirt.SetFlags), ClearFlags: uint64(resp.Libvirt.ClearFlags), Params: resp.Libvirt.Params},
		},
	}, nil
}

type nodeHookAdapter struct {
	handlers map[NodeHookPoint]NodeHookHandler
}

func (a *nodeHookAdapter) ExecuteNodeHook(ctx context.Context, req *pb.ExecuteNodeHookRequest) (*pb.ExecuteNodeHookResponse, error) {
	hookPoint := NodeHookPoint(req.GetHookPoint())
	handler, ok := a.handlers[hookPoint]
	if !ok {
		return nil, status.Errorf(codes.Unimplemented, "no handler registered for hook point %q", hookPoint)
	}

	var vmi v1.VirtualMachineInstance
	if err := json.Unmarshal(req.GetVmi(), &vmi); err != nil {
		return nil, fmt.Errorf("failed to unmarshal VMI: %w", err)
	}

	hookReq := &NodeHookRequest{
		HookPoint: hookPoint,
		VMI:       &vmi,
		NodeName:  req.GetNodeContext().GetNodeName(),
	}

	if _, err := handler.ExecuteNodeHook(ctx, hookReq); err != nil {
		return nil, fmt.Errorf("node hook handler failed: %w", err)
	}

	return &pb.ExecuteNodeHookResponse{}, nil
}

// recoverPanics turns a handler panic into a codes.Internal error instead of
// crashing the server goroutine.
func recoverPanics() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (_ any, err error) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("panic serving %s: %v\n%s", info.FullMethod, r, debug.Stack())
				err = status.Errorf(codes.Internal, "plugin handler panicked")
			}
		}()
		return handler(ctx, req)
	}
}

const (
	roleSidecar       = "sidecar"
	roleDaemonSet     = "daemonset"
	domainTypeLibvirt = "libvirt"
)

type serveOption func(*serveOptions)

type serveOptions struct {
	launcherSocketPath string
	nodeSocketPath     string
	stopCh             <-chan struct{}
	entrypoint         string
	role               string
}

func withLauncherSocketPath(path string) serveOption {
	return func(options *serveOptions) { options.launcherSocketPath = path }
}

func withNodeSocketPath(path string) serveOption {
	return func(options *serveOptions) { options.nodeSocketPath = path }
}

func withStopCh(ch <-chan struct{}) serveOption {
	return func(options *serveOptions) { options.stopCh = ch }
}

func withEntrypoint(entrypoint string) serveOption {
	return func(options *serveOptions) { options.entrypoint = entrypoint }
}

func withRole(role string) serveOption {
	return func(options *serveOptions) { options.role = role }
}

func (p *Plugin) launcherHooksForEntrypoint(entrypoint string) []LauncherHookOption {
	var result []LauncherHookOption
	for _, launcherHook := range p.launcherHooks {
		if launcherHook.isCEL() {
			continue
		}
		if p.resolveEntrypoint(launcherHook.entrypoint) == entrypoint {
			result = append(result, launcherHook)
		}
	}
	return result
}

func (p *Plugin) nodeHooksForEntrypoint(entrypoint string) []NodeHookOption {
	var result []NodeHookOption
	for _, nodeHook := range p.nodeHooks {
		if p.resolveEntrypoint(nodeHook.entrypoint) == entrypoint {
			result = append(result, nodeHook)
		}
	}
	return result
}

func (p *Plugin) allEntrypoints() []string {
	seen := map[string]bool{}
	var entrypoints []string
	for _, launcherHook := range p.launcherHooks {
		if launcherHook.isCEL() {
			continue
		}
		entrypoint := p.resolveEntrypoint(launcherHook.entrypoint)
		if !seen[entrypoint] {
			seen[entrypoint] = true
			entrypoints = append(entrypoints, entrypoint)
		}
	}
	for _, nodeHook := range p.nodeHooks {
		entrypoint := p.resolveEntrypoint(nodeHook.entrypoint)
		if !seen[entrypoint] {
			seen[entrypoint] = true
			entrypoints = append(entrypoints, entrypoint)
		}
	}
	return entrypoints
}

// Serve starts the gRPC servers for the registered hooks and blocks until
// a signal or stop channel triggers shutdown.
func (p *Plugin) Serve(opts ...serveOption) error {
	options := &serveOptions{}
	for _, opt := range opts {
		opt(options)
	}

	var launcherHooks []LauncherHookOption
	var nodeHooks []NodeHookOption
	var effectiveEntrypoint string

	if options.entrypoint != "" {
		effectiveEntrypoint = options.entrypoint
		launcherHooks = p.launcherHooksForEntrypoint(effectiveEntrypoint)
		nodeHooks = p.nodeHooksForEntrypoint(effectiveEntrypoint)
	} else {
		allEntrypoints := p.allEntrypoints()
		if len(allEntrypoints) == 0 {
			hasCELOnly := len(p.launcherHooks) > 0 && len(p.sidecarLauncherHooks()) == 0 && len(p.nodeHooks) == 0
			if hasCELOnly {
				return fmt.Errorf("plugin has only CEL launcher hooks which are evaluated by kubevirt directly; Serve() is not needed")
			}
			return fmt.Errorf("no hooks registered")
		}
		if len(allEntrypoints) > 1 {
			return fmt.Errorf("multiple entrypoints found (%v); use --entrypoint to select one", allEntrypoints)
		}
		effectiveEntrypoint = allEntrypoints[0]
		launcherHooks = p.launcherHooksForEntrypoint(effectiveEntrypoint)
		nodeHooks = p.nodeHooksForEntrypoint(effectiveEntrypoint)
	}

	switch options.role {
	case roleSidecar:
		if len(launcherHooks) == 0 {
			return fmt.Errorf("no launcher hooks registered for entrypoint %q; nothing to serve with --role %s", effectiveEntrypoint, roleSidecar)
		}
		nodeHooks = nil
	case roleDaemonSet:
		if len(nodeHooks) == 0 {
			return fmt.Errorf("no node hooks registered for entrypoint %q; nothing to serve with --role %s", effectiveEntrypoint, roleDaemonSet)
		}
		launcherHooks = nil
	default:
		if len(launcherHooks) > 0 && len(nodeHooks) > 0 {
			return fmt.Errorf("entrypoint %q has both launcher and node hooks; use --role %s or --role %s to select which to serve", effectiveEntrypoint, roleSidecar, roleDaemonSet)
		}
	}

	if len(launcherHooks) > 0 && options.launcherSocketPath == "" {
		options.launcherSocketPath = LauncherSocketPath(p.name, effectiveEntrypoint)
	}
	if len(nodeHooks) > 0 && options.nodeSocketPath == "" {
		options.nodeSocketPath = NodeSocketPath(p.name, effectiveEntrypoint)
	}

	errCh := make(chan error, 2)
	var servers []*grpc.Server

	if len(launcherHooks) > 0 {
		adapter := &launcherHookAdapter{}
		for _, launcherHook := range launcherHooks {
			switch launcherHook.hookPoint {
			case LauncherHookGuestDefinition:
				adapter.guestDefinitionHandler = launcherHook.guestDefinitionHandler
			case LauncherHookPreBoot:
				adapter.preBootHandler = launcherHook.preBootHandler
			case LauncherHookPreMigrationSource:
				adapter.preMigrationSourceHandler = launcherHook.preMigrationSourceHandler
			default:
				panic(fmt.Sprintf("unknown launcher hook point %q", launcherHook.hookPoint))
			}
		}

		server := grpc.NewServer(grpc.UnaryInterceptor(recoverPanics()))
		pb.RegisterLauncherHookServiceServer(server, adapter)

		listener, err := listenUnix(options.launcherSocketPath)
		if err != nil {
			return fmt.Errorf("launcher hook socket: %w", err)
		}
		servers = append(servers, server)
		go func() { errCh <- server.Serve(listener) }()
	}

	if len(nodeHooks) > 0 {
		server := grpc.NewServer(grpc.UnaryInterceptor(recoverPanics()))
		handlers := make(map[NodeHookPoint]NodeHookHandler, len(nodeHooks))
		for _, nodeHook := range nodeHooks {
			handlers[nodeHook.hookPoint] = nodeHook.handler
		}
		pb.RegisterNodeHookServiceServer(server, &nodeHookAdapter{handlers: handlers})

		listener, err := listenUnix(options.nodeSocketPath)
		if err != nil {
			for _, srv := range servers {
				srv.Stop()
			}
			return fmt.Errorf("node hook socket: %w", err)
		}
		servers = append(servers, server)
		go func() { errCh <- server.Serve(listener) }()
	}

	if len(servers) == 0 {
		return fmt.Errorf("no hooks registered for entrypoint %q", effectiveEntrypoint)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigCh)

	select {
	case <-sigCh:
	case <-options.stopCh:
	case err := <-errCh:
		for _, server := range servers {
			server.Stop()
		}
		return err
	}

	done := make(chan struct{})
	go func() {
		for _, server := range servers {
			server.GracefulStop()
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		for _, server := range servers {
			server.Stop()
		}
		<-done
	}

	return nil
}

func listenUnix(path string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("failed to create socket directory: %w", err)
	}
	// The os.Remove error is intentionally ignored; it only clears a stale socket from a previous run.
	os.Remove(path)
	return net.Listen("unix", path)
}

// Execute is the CLI entry point. It dispatches to "serve" or "generate"
// subcommands based on os.Args.
func (p *Plugin) Execute() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <serve|generate> [args...]\n", os.Args[0])
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		serveOpts, err := parseServeArgs(os.Args[2:])
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		if err != nil {
			log.Fatalf("serve failed: %v", err)
		}
		if err := p.Serve(serveOpts...); err != nil {
			log.Fatalf("serve failed: %v", err)
		}
	case "generate":
		outputDir := "deploy"
		if len(os.Args) > 2 {
			outputDir = os.Args[2]
		}
		if err := p.Generate(outputDir); err != nil {
			log.Fatalf("generate failed: %v", err)
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown subcommand %q. Usage: %s <serve|generate> [args...]\n", os.Args[1], os.Args[0])
		os.Exit(1)
	}
}

func parseServeArgs(args []string) ([]serveOption, error) {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	entrypoint := flags.String("entrypoint", "", "serve only the hooks of this entrypoint")
	role := flags.String("role", "", fmt.Sprintf("serve only the launcher hooks (%s) or only the node hooks (%s)", roleSidecar, roleDaemonSet))
	if err := flags.Parse(args); err != nil {
		return nil, err
	}

	if flags.NArg() > 0 {
		return nil, fmt.Errorf("unexpected arguments %v", flags.Args())
	}

	if *role != "" && *role != roleSidecar && *role != roleDaemonSet {
		return nil, fmt.Errorf("unknown role %q; must be %q or %q", *role, roleSidecar, roleDaemonSet)
	}

	return []serveOption{withEntrypoint(*entrypoint), withRole(*role)}, nil
}
