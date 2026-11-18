package main

import (
	"context"
	"log"

	"github.com/kubevirt/plugins/pkg/sdk/plugin"
)

type nodeHook struct{}

func (*nodeHook) ExecuteNodeHook(_ context.Context, req *plugin.NodeHookRequest) (*plugin.NodeHookResponse, error) {
	log.Printf("node hook %s vmi=%s/%s", req.HookPoint, req.VMI.Namespace, req.VMI.Name)
	return nil, nil
}

func newPlugin() *plugin.Plugin {
	return plugin.New("test-node-hook-lifecycle").
		WithNodeHook(plugin.NodeHookPreVMStart, plugin.NodeHandler(&nodeHook{})).
		WithNodeHook(plugin.NodeHookOnVMStop, plugin.NodeHandler(&nodeHook{})).
		WithNodeHook(plugin.NodeHookPostVMStop, plugin.NodeHandler(&nodeHook{}))
}

func main() {
	newPlugin().Execute()
}
