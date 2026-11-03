package main

import (
	"context"

	"github.com/kubevirt/plugins/pkg/sdk/plugin"
	v1 "kubevirt.io/api/core/v1"
	"libvirt.org/go/libvirtxml"
)

type domainMutator struct{}

func (*domainMutator) MutateDomain(_ context.Context, domain *libvirtxml.Domain, _ *v1.VirtualMachineInstance) error {
	domain.Description = "mutated-by-test-domain-mutator"
	return nil
}

func main() {
	plugin.New("test-domain-mutator").
		WithDomainHook(plugin.ForLibvirt(&domainMutator{})).
		Execute()
}
