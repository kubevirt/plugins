package main

import (
	"context"

	"github.com/kubevirt/plugins/pkg/sdk/plugin"
)

type domainHook struct{}

func (*domainHook) GuestDefinition(_ context.Context, _ *plugin.GuestDefinitionRequest) error {
	return nil
}

type nodeHook struct{}

func (*nodeHook) ExecuteNodeHook(_ context.Context, _ *plugin.NodeHookRequest) (*plugin.NodeHookResponse, error) {
	return nil, nil
}

func main() {
	plugin.New("test-mixed-hooks").
		WithLauncherHook(plugin.GuestDefinition(&domainHook{})).
		WithNodeHook(plugin.NodeHookPreVMStart, plugin.NodeHandler(&nodeHook{})).
		Execute()
}
