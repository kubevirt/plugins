package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/kubevirt/plugins/pkg/sdk/plugin"
)

type entrypointMutator struct {
	marker string
}

// GuestDefinition may run several times per VM start and see its own earlier
// output, so the marker is only appended when missing.
func (h *entrypointMutator) GuestDefinition(_ context.Context, req *plugin.GuestDefinitionRequest) error {
	if strings.Contains(req.Domain.Description, h.marker) {
		return nil
	}
	if req.Domain.Description == "" {
		req.Domain.Description = h.marker
	} else {
		req.Domain.Description = fmt.Sprintf("%s,%s", req.Domain.Description, h.marker)
	}
	return nil
}

func main() {
	plugin.New("test-multi-entrypoint").
		WithLauncherHook(plugin.GuestDefinition(&entrypointMutator{marker: "foo"}).WithEntrypoint("foo")).
		WithLauncherHook(plugin.GuestDefinition(&entrypointMutator{marker: "bar"}).WithEntrypoint("bar")).
		Execute()
}
