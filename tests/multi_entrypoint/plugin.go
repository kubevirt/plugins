package main

import (
	"context"
	"fmt"

	"github.com/iholder101/kubevirt-plugins/pkg/sdk/plugin"
	"libvirt.org/go/libvirtxml"
	v1 "kubevirt.io/api/core/v1"
)

type entrypointMutator struct {
	marker string
}

func (h *entrypointMutator) MutateDomain(_ context.Context, domain *libvirtxml.Domain, _ *v1.VirtualMachineInstance) error {
	if domain.Description == "" {
		domain.Description = h.marker
	} else {
		domain.Description = fmt.Sprintf("%s,%s", domain.Description, h.marker)
	}
	return nil
}

func main() {
	plugin.New("test-multi-entrypoint").
		WithDomainHook(plugin.ForLibvirt(&entrypointMutator{marker: "foo"}).WithEntrypoint("foo")).
		WithDomainHook(plugin.ForLibvirt(&entrypointMutator{marker: "bar"}).WithEntrypoint("bar")).
		Execute()
}
