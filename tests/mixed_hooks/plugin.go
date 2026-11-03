package main

import (
	"context"

	"github.com/kubevirt/plugins/pkg/sdk/plugin"
	v1 "kubevirt.io/api/core/v1"
	"libvirt.org/go/libvirtxml"
)

type domainHook struct{}

func (*domainHook) MutateDomain(_ context.Context, _ *libvirtxml.Domain, _ *v1.VirtualMachineInstance) error {
	return nil
}

type nodeHook struct{}

func (*nodeHook) ExecuteNodeHook(_ context.Context, _ *plugin.NodeHookRequest) error {
	return nil
}

func main() {
	plugin.New("test-mixed-hooks").
		WithDomainHook(plugin.ForLibvirt(&domainHook{})).
		WithNodeHook(plugin.PreVMStart, plugin.NodeHandler(&nodeHook{})).
		Execute()
}
