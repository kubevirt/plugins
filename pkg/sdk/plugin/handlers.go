package plugin

import (
	"context"

	v1 "kubevirt.io/api/core/v1"
	"libvirt.org/go/libvirtxml"
)

type LibvirtDomainHookHandler interface {
	MutateDomain(ctx context.Context, domain *libvirtxml.Domain, vmi *v1.VirtualMachineInstance) error
}

type NodeHookHandler interface {
	ExecuteNodeHook(ctx context.Context, req *NodeHookRequest) error
}
