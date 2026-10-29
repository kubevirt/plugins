package main

import (
	"context"

	"github.com/kubevirt/plugins/pkg/sdk/plugin"
)

type domainMutator struct{}

// GuestDefinition may run several times per VM start, so the mutation is a
// fixed value rather than an append.
func (*domainMutator) GuestDefinition(_ context.Context, req *plugin.GuestDefinitionRequest) error {
	req.Domain.Description = "mutated-by-test-domain-mutator"
	return nil
}

func main() {
	plugin.New("test-domain-mutator").
		WithLauncherHook(plugin.GuestDefinition(&domainMutator{})).
		Execute()
}
