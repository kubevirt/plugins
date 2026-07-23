package main

import (
	"context"

	"github.com/iholder101/kubevirt-plugins/pkg/sdk/plugin"
	"libvirt.org/go/libvirtxml"
	v1 "kubevirt.io/api/core/v1"
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
