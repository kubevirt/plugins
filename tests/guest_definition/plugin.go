package main

import (
	"context"
	"log"
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/kubevirt/plugins/pkg/sdk/plugin"
)

type guestDefinitionHook struct{}

// GuestDefinition may run several times per VM start and see its own earlier
// output, so the description is set to a fixed value rather than appended to.
func (*guestDefinitionHook) GuestDefinition(_ context.Context, req *plugin.GuestDefinitionRequest) error {
	log.Printf("GuestDefinition invocationContext=%s vmi=%s/%s", req.InvocationContext, req.VMI.Namespace, req.VMI.Name)
	req.Domain.Description = "sdk-guest-definition"
	return nil
}

func newPlugin() *plugin.Plugin {
	return plugin.New("test-guest-definition").
		WithLauncherHook(plugin.GuestDefinition(&guestDefinitionHook{}))
}

// gatedPluginName is the plugin whose sidecar injection the generated MAP
// restricts to virt-launcher pods labelled sdk-sidecar-gate=true.
const gatedPluginName = "test-guest-definition-gated"

type gatedGuestDefinitionHook struct{}

func (*gatedGuestDefinitionHook) GuestDefinition(_ context.Context, req *plugin.GuestDefinitionRequest) error {
	log.Printf("GuestDefinition invocationContext=%s vmi=%s/%s", req.InvocationContext, req.VMI.Namespace, req.VMI.Name)
	req.Domain.Description = "sdk-guest-definition-gated"
	return nil
}

// newGatedPlugin scopes the sidecar injection to labelled launcher pods and
// gates the hook on the same label, so unlabelled VMIs neither get the
// sidecar nor wait for its socket.
func newGatedPlugin() *plugin.Plugin {
	return plugin.New(gatedPluginName).
		WithLauncherHook(plugin.GuestDefinition(&gatedGuestDefinitionHook{}).WithCondition(`"sdk-sidecar-gate" in vmi.Labels`)).
		WithSidecarPodLabels(map[string]string{"sdk-sidecar-gate": "true"}).
		WithSidecarResources(corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("50m"), corev1.ResourceMemory: resource.MustParse("64Mi")}})
}

func main() {
	// Both plugins of this suite ship in one image; the injected sidecar
	// args pass the plugin name as the entrypoint, which selects the one
	// to serve.
	pluginToServe := newPlugin()
	for i := 0; i+1 < len(os.Args); i++ {
		if os.Args[i] == "--entrypoint" && os.Args[i+1] == gatedPluginName {
			pluginToServe = newGatedPlugin()
		}
	}
	pluginToServe.Execute()
}
