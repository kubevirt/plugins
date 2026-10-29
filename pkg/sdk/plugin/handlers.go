package plugin

import (
	"context"

	v1 "kubevirt.io/api/core/v1"
	"libvirt.org/go/libvirtxml"
)

type LibvirtDomainHookHandler interface {
	MutateDomain(ctx context.Context, domain *libvirtxml.Domain, vmi *v1.VirtualMachineInstance) error
}

// NodeHookResponse is the response of a node hook. It is empty today, but
// KubeVirt's ExecuteNodeHookResponse can grow fields, and mapping them here
// keeps handler signatures stable.
type NodeHookResponse struct{}

type NodeHookHandler interface {
	ExecuteNodeHook(ctx context.Context, req *NodeHookRequest) (*NodeHookResponse, error)
}
