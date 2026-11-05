package plugin

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pb "github.com/kubevirt/plugins/api/hooks/v1alpha1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type rawHandlerFunc func(context.Context, *pb.MutateDomainRequest) (*pb.MutateDomainResponse, error)

func (f rawHandlerFunc) MutateDomain(ctx context.Context, req *pb.MutateDomainRequest) (*pb.MutateDomainResponse, error) {
	return f(ctx, req)
}

func rawConnection(t *testing.T, handler pb.DomainHookServiceServer) *grpc.ClientConn {
	t.Helper()
	// Keep the pathname below the Unix socket length limit, even in a deep checkout.
	dir, err := os.MkdirTemp("", "sdk-raw-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "domain.sock")
	stop := make(chan struct{})
	done := make(chan error, 1)
	p := New("test").WithDomainHook(ForRawDomain(handler))
	go func() { done <- p.Serve(withDomainSocketPath(socket), withStopCh(stop)) }()
	t.Cleanup(func() { close(stop); awaitServeStop(t, done) })
	waitForSocket(t, socket)
	return dialSocket(t, socket)
}

func TestRawDomainHookPreservesWireBytes(t *testing.T) {
	input := []byte("<domain type='kvm' xmlns:custom='urn:test'>\n<!--keep--><features><hyperv><vsm state='on'/><xmm_output state='on'/></hyperv></features><custom:unknown value='keep'/><title>before</title></domain>")
	vmi := []byte(`{ "metadata": {"name":"test"}, "unknown":"keep" }`)
	for _, mutate := range []bool{false, true} {
		name := "noop"
		if mutate {
			name = "selective-mutation"
		}
		t.Run(name, func(t *testing.T) {
			want := input
			if mutate {
				want = bytes.Replace(input, []byte("<title>before</title>"), []byte("<title>after</title>"), 1)
			}
			handler := rawHandlerFunc(func(ctx context.Context, req *pb.MutateDomainRequest) (*pb.MutateDomainResponse, error) {
				if !bytes.Equal(req.Vmi, vmi) || InvocationContextFromContext(ctx) != "test-context" {
					t.Error("raw VMI bytes or invocation context changed")
				}
				if req.GetDomainType() != "libvirt" {
					return nil, status.Error(codes.InvalidArgument, "raw handler requires libvirt")
				}
				if !bytes.Equal(req.Domain, input) && !bytes.Equal(req.Domain, want) {
					t.Error("raw domain bytes changed before reaching the handler")
				}
				output := req.Domain
				if mutate {
					output = bytes.Replace(output, []byte("<title>before</title>"), []byte("<title>after</title>"), 1)
				}
				return &pb.MutateDomainResponse{Domain: output}, nil
			})
			connection := rawConnection(t, handler)
			client := pb.NewDomainHookServiceClient(connection)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			req := &pb.MutateDomainRequest{DomainType: "libvirt", Domain: input, Vmi: vmi, SidecarContext: &pb.SidecarContext{InvocationContext: "test-context"}}
			response := &pb.MutateDomainResponse{}
			// Pin the launcher RPC contract independently of the generated client.
			if err := connection.Invoke(ctx, "/kubevirt.hooks.plugins.v1alpha1.DomainHookService/MutateDomain", req, response); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(response.Domain, want) || !bytes.Equal(req.Domain, input) {
				t.Fatal("raw hook rewrote unrelated XML or changed its caller's input")
			}
			req.Domain = response.Domain
			again, err := client.MutateDomain(ctx, req)
			if err != nil || !bytes.Equal(again.GetDomain(), want) {
				t.Fatalf("repeat call changed output: %v", err)
			}
			req.DomainType = "unsupported"
			_, err = client.MutateDomain(ctx, req)
			if status.Code(err) != codes.InvalidArgument || status.Convert(err).Message() != "raw handler requires libvirt" {
				t.Fatalf("raw handler status changed: %v", err)
			}
		})
	}
}

func TestRawDomainHookReceivesCancellation(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	handler := rawHandlerFunc(func(ctx context.Context, _ *pb.MutateDomainRequest) (*pb.MutateDomainResponse, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return nil, status.FromContextError(ctx.Err()).Err()
	})
	client := pb.NewDomainHookServiceClient(rawConnection(t, handler))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := client.MutateDomain(ctx, &pb.MutateDomainRequest{DomainType: "libvirt"})
		done <- err
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("raw handler was not invoked")
	}
	cancel()
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("raw handler did not receive cancellation")
	}
	select {
	case err := <-done:
		if status.Code(err) != codes.Canceled {
			t.Fatalf("unexpected cancellation status: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled RPC did not return")
	}
}

func TestRawDomainHookLeavesValidationToHandler(t *testing.T) {
	request := &pb.MutateDomainRequest{DomainType: "custom", Domain: []byte("unparsed domain"), Vmi: []byte("unparsed VMI")}
	ctx := context.WithValue(context.Background(), contextKey("caller"), "keep")
	adapter := &rawDomainHookAdapter{handler: rawHandlerFunc(func(received context.Context, req *pb.MutateDomainRequest) (*pb.MutateDomainResponse, error) {
		if req != request || received.Value(contextKey("caller")) != "keep" || InvocationContextFromContext(received) != "" {
			t.Error("request or context changed")
		}
		return &pb.MutateDomainResponse{Domain: req.Domain}, nil
	})}
	response, err := adapter.MutateDomain(ctx, request)
	if err != nil || !bytes.Equal(response.GetDomain(), request.Domain) {
		t.Fatalf("raw adapter attempted validation or serialization: %v", err)
	}
}

func TestRawDomainHookRequiresDedicatedEntrypoint(t *testing.T) {
	handler := rawHandlerFunc(func(_ context.Context, req *pb.MutateDomainRequest) (*pb.MutateDomainResponse, error) {
		return &pb.MutateDomainResponse{Domain: req.Domain}, nil
	})
	for name, hooks := range map[string][]*DomainHookOption{
		"raw-typed": {ForRawDomain(handler), ForLibvirt(&stubDomainHandler{})},
		"typed-raw": {ForLibvirt(&stubDomainHandler{}), ForRawDomain(handler)},
		"raw-raw":   {ForRawDomain(handler), ForRawDomain(handler)},
	} {
		t.Run(name, func(t *testing.T) {
			p := New("test")
			for _, hook := range hooks {
				p.WithDomainHook(hook)
			}
			if err := p.Serve(); err == nil || !strings.Contains(err.Error(), "dedicated entrypoint") {
				t.Fatalf("ambiguous raw domain chain accepted: %v", err)
			}
		})
	}
	p := New("test").
		WithDomainHook(ForRawDomain(handler).WithEntrypoint("raw")).
		WithDomainHook(ForLibvirt(&stubDomainHandler{}).WithEntrypoint("typed"))
	manifest, err := p.renderPluginCR()
	if err != nil || !strings.Contains(manifest, "/raw/domain.sock") || !strings.Contains(manifest, "/typed/domain.sock") {
		t.Fatalf("separate raw and typed entrypoints did not render: %v\n%s", err, manifest)
	}
}

func TestDomainHookRequiresExactlyOneImplementation(t *testing.T) {
	handler := rawHandlerFunc(func(context.Context, *pb.MutateDomainRequest) (*pb.MutateDomainResponse, error) { return nil, nil })
	for name, option := range map[string]*DomainHookOption{
		"empty":     {},
		"nil-raw":   ForRawDomain(nil),
		"raw-typed": {handler: &stubDomainHandler{}, rawHandler: handler},
		"raw-CEL":   {rawHandler: handler, expression: "object"},
		"typed-CEL": {handler: &stubDomainHandler{}, expression: "object"},
		"all":       {handler: &stubDomainHandler{}, rawHandler: handler, expression: "object"},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("invalid implementation selection accepted")
				}
			}()
			New("test").WithDomainHook(option)
		})
	}
}
