package main

import (
	"context"

	"github.com/iholder101/kubevirt-plugins/pkg/sdk/plugin"
)

type nodeHook struct{}

func (*nodeHook) ExecuteNodeHook(_ context.Context, _ *plugin.NodeHookRequest) error {
	return nil
}

func main() {
	plugin.New("test-node-hook").
		WithNodeHook(plugin.PreVMStart, plugin.NodeHandler(&nodeHook{})).
		Execute()
}
