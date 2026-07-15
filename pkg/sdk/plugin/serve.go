package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	pb "github.com/iholder101/kubevirt-plugins/api/hooks/v1alpha1"
	"google.golang.org/grpc"
	v1 "kubevirt.io/api/core/v1"
	"libvirt.org/go/libvirtxml"
)

type contextKey string

const invocationContextKey contextKey = "invocationContext"

func InvocationContextFromContext(ctx context.Context) string {
	val, _ := ctx.Value(invocationContextKey).(string)
	return val
}

type domainHookAdapter struct {
	handler LibvirtDomainHookHandler
}

func (a *domainHookAdapter) MutateDomain(ctx context.Context, req *pb.MutateDomainRequest) (*pb.MutateDomainResponse, error) {
	invCtx := req.GetSidecarContext().GetInvocationContext()
	ctx = context.WithValue(ctx, invocationContextKey, invCtx)

	if req.GetDomainType() != "libvirt" {
		return &pb.MutateDomainResponse{Domain: req.GetDomain()}, nil
	}

	var domain libvirtxml.Domain
	if err := domain.Unmarshal(string(req.GetDomain())); err != nil {
		return nil, fmt.Errorf("failed to unmarshal domain XML: %w", err)
	}

	var vmi v1.VirtualMachineInstance
	if err := json.Unmarshal(req.GetVmi(), &vmi); err != nil {
		return nil, fmt.Errorf("failed to unmarshal VMI: %w", err)
	}

	if err := a.handler.MutateDomain(ctx, &domain, &vmi); err != nil {
		return nil, fmt.Errorf("domain hook handler failed: %w", err)
	}

	xmlStr, err := domain.Marshal()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal domain XML: %w", err)
	}

	return &pb.MutateDomainResponse{Domain: []byte(xmlStr)}, nil
}

type nodeHookAdapter struct {
	handlers map[string]NodeHookHandler
}

func (a *nodeHookAdapter) ExecuteNodeHook(ctx context.Context, req *pb.ExecuteNodeHookRequest) (*pb.ExecuteNodeHookResponse, error) {
	handler, ok := a.handlers[req.GetHookPoint()]
	if !ok {
		return nil, fmt.Errorf("no handler registered for hook point %q", req.GetHookPoint())
	}

	var vmi v1.VirtualMachineInstance
	if err := json.Unmarshal(req.GetVmi(), &vmi); err != nil {
		return nil, fmt.Errorf("failed to unmarshal VMI: %w", err)
	}

	hookReq := &NodeHookRequest{
		HookPoint: req.GetHookPoint(),
		VMI:       &vmi,
		NodeName:  req.GetNodeContext().GetNodeName(),
	}

	if err := handler.ExecuteNodeHook(ctx, hookReq); err != nil {
		return nil, fmt.Errorf("node hook handler failed: %w", err)
	}

	return &pb.ExecuteNodeHookResponse{}, nil
}

type serveOption func(*serveOptions)

type serveOptions struct {
	domainSocketPath string
	nodeSocketPath   string
	stopCh           <-chan struct{}
	entrypoint       string
}

func withDomainSocketPath(path string) serveOption {
	return func(options *serveOptions) { options.domainSocketPath = path }
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

type chainedDomainHandler struct {
	handlers []LibvirtDomainHookHandler
}

func (h *chainedDomainHandler) MutateDomain(ctx context.Context, domain *libvirtxml.Domain, vmi *v1.VirtualMachineInstance) error {
	for _, handler := range h.handlers {
		if err := handler.MutateDomain(ctx, domain, vmi); err != nil {
			return err
		}
	}
	return nil
}

func (p *Plugin) domainHooksForEntrypoint(entrypoint string) []DomainHookOption {
	var result []DomainHookOption
	for _, domainHook := range p.domainHooks {
		if domainHook.isCEL() {
			continue
		}
		if p.resolveEntrypoint(domainHook.entrypoint) == entrypoint {
			result = append(result, domainHook)
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
	for _, domainHook := range p.domainHooks {
		if domainHook.isCEL() {
			continue
		}
		entrypoint := p.resolveEntrypoint(domainHook.entrypoint)
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

	var domainHooks []DomainHookOption
	var nodeHooks []NodeHookOption
	var effectiveEntrypoint string

	if options.entrypoint != "" {
		effectiveEntrypoint = options.entrypoint
		domainHooks = p.domainHooksForEntrypoint(effectiveEntrypoint)
		nodeHooks = p.nodeHooksForEntrypoint(effectiveEntrypoint)
	} else {
		allEntrypoints := p.allEntrypoints()
		if len(allEntrypoints) == 0 {
			hasCELOnly := len(p.domainHooks) > 0 && len(p.sidecarDomainHooks()) == 0 && len(p.nodeHooks) == 0
			if hasCELOnly {
				return fmt.Errorf("plugin has only CEL domain hooks which are evaluated by kubevirt directly; Serve() is not needed")
			}
			return fmt.Errorf("no hooks registered")
		}
		if len(allEntrypoints) > 1 {
			return fmt.Errorf("multiple entrypoints found (%v); use --entrypoint to select one", allEntrypoints)
		}
		effectiveEntrypoint = allEntrypoints[0]
		domainHooks = p.domainHooksForEntrypoint(effectiveEntrypoint)
		nodeHooks = p.nodeHooksForEntrypoint(effectiveEntrypoint)
	}

	if len(domainHooks) > 0 && options.domainSocketPath == "" {
		options.domainSocketPath = DomainSocketPathForEntrypoint(p.name, effectiveEntrypoint)
	}
	if len(nodeHooks) > 0 && options.nodeSocketPath == "" {
		options.nodeSocketPath = NodeSocketPathForEntrypoint(p.name, effectiveEntrypoint)
	}

	errCh := make(chan error, 2)
	var servers []*grpc.Server

	if len(domainHooks) > 0 {
		var handler LibvirtDomainHookHandler
		if len(domainHooks) == 1 {
			handler = domainHooks[0].handler
		} else {
			handlers := make([]LibvirtDomainHookHandler, len(domainHooks))
			for i, domainHook := range domainHooks {
				handlers[i] = domainHook.handler
			}
			handler = &chainedDomainHandler{handlers: handlers}
		}

		server := grpc.NewServer()
		pb.RegisterDomainHookServiceServer(server, &domainHookAdapter{handler: handler})

		listener, err := listenUnix(options.domainSocketPath)
		if err != nil {
			return fmt.Errorf("domain hook socket: %w", err)
		}
		servers = append(servers, server)
		go func() { errCh <- server.Serve(listener) }()
	}

	if len(nodeHooks) > 0 {
		server := grpc.NewServer()
		handlers := make(map[string]NodeHookHandler, len(nodeHooks))
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
	}

	return nil
}

func listenUnix(path string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("failed to create socket directory: %w", err)
	}
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
		var serveOpts []serveOption
		for i := 2; i < len(os.Args); i++ {
			if os.Args[i] == "--entrypoint" && i+1 < len(os.Args) {
				serveOpts = append(serveOpts, withEntrypoint(os.Args[i+1]))
				i++
			}
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
