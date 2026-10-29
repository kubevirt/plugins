package main

import (
	"context"

	"github.com/kubevirt/plugins/pkg/sdk/plugin"
)

type nodeHook struct{}

func (*nodeHook) ExecuteNodeHook(_ context.Context, _ *plugin.NodeHookRequest) (*plugin.NodeHookResponse, error) {
	return nil, nil
}

func main() {
	plugin.New("test-node-hook").
		WithNodeHook(plugin.NodeHookPreVMStart, plugin.NodeHandler(&nodeHook{})).
		Execute()
}
