package plugin

import (
	"context"

	pb "github.com/kubevirt/plugins/api/hooks/v1alpha1"
	"github.com/kubevirt/plugins/pkg/sdk/migration"
	v1 "kubevirt.io/api/core/v1"
	"libvirt.org/go/libvirtxml"
)

// LibvirtDomainMigrateParameters re-exports the generated migration
// parameters so PreMigrationSource authors need only SDK packages.
type LibvirtDomainMigrateParameters = pb.LibvirtDomainMigrateParameters

// GuestDefinitionRequest is the input to a GuestDefinition handler.
type GuestDefinitionRequest struct {
	// Domain is the libvirt domain about to be defined; the handler mutates it in place.
	Domain *libvirtxml.Domain
	// VMI is the VirtualMachineInstance the domain is built from.
	VMI *v1.VirtualMachineInstance
	// InvocationContext tells why the hook runs (Boot or MigrationTarget).
	InvocationContext InvocationContext
}

// GuestDefinitionHandler mutates the domain XML before KubeVirt defines it.
// GuestDefinition may run 2-3 times per VM start and see its own earlier
// output, so handlers must be idempotent; only fields modeled by KubeVirt's
// libvirtxml survive the round trip back to the launcher.
type GuestDefinitionHandler interface {
	GuestDefinition(ctx context.Context, req *GuestDefinitionRequest) error
}

// PreBootRequest is the input to a PreBoot handler.
type PreBootRequest struct {
	// VMI is the VirtualMachineInstance about to start.
	VMI *v1.VirtualMachineInstance
	// ArtifactPaths maps PreBoot artifact keys to their absolute paths in the launcher; always non-nil, but may be empty.
	ArtifactPaths map[string]string
}

// CloudInitISO returns the path of the generated cloud-init ISO, when present.
func (r *PreBootRequest) CloudInitISO() (string, bool) {
	path, ok := r.ArtifactPaths[PreBootArtifactCloudInitISO]
	return path, ok
}

// EphemeralDiskDir returns the ephemeral disk directory, when present.
func (r *PreBootRequest) EphemeralDiskDir() (string, bool) {
	path, ok := r.ArtifactPaths[PreBootArtifactEphemeralDiskDir]
	return path, ok
}

// PreBootResponse is the response of a PreBoot hook. It is empty today, but
// KubeVirt's PreBootResponse can grow fields, and mapping them here keeps
// handler signatures stable.
type PreBootResponse struct{}

// PreBootHandler runs after the boot artifacts were generated and before the
// guest starts. It may repeat after a failed start and filesystem changes are
// never rolled back: honor ctx, write transactionally and stay idempotent,
// and prefer the Fail strategy so a timed-out hook does not keep writing
// while the launcher proceeds.
type PreBootHandler interface {
	PreBoot(ctx context.Context, req *PreBootRequest) (*PreBootResponse, error)
}

// LibvirtMigrationConfig is the libvirt migration configuration KubeVirt is
// about to hand to libvirt. Params is never nil.
type LibvirtMigrationConfig struct {
	Flags  migration.Flags
	Params *LibvirtDomainMigrateParameters
}

// PreMigrationSourceRequest is the input to a PreMigrationSource handler.
type PreMigrationSourceRequest struct {
	VMI *v1.VirtualMachineInstance
	// Libvirt is nil only when the launcher sent a non-libvirt configuration.
	Libvirt *LibvirtMigrationConfig
}

// LibvirtMigrationChanges describes the changes a PreMigrationSource handler
// asks KubeVirt to apply; see package migration for the tri-state encoding.
type LibvirtMigrationChanges struct {
	SetFlags   migration.Flags
	ClearFlags migration.Flags
	Params     *LibvirtDomainMigrateParameters
}

// PreMigrationSourceResponse carries only the changes to the migration
// configuration; a nil response, or one with a nil Libvirt, changes nothing.
type PreMigrationSourceResponse struct {
	Libvirt *LibvirtMigrationChanges
}

// PreMigrationSourceHandler adjusts a migration just before libvirt is asked
// to migrate; the response returns only changes (see package migration).
type PreMigrationSourceHandler interface {
	PreMigrationSource(ctx context.Context, req *PreMigrationSourceRequest) (*PreMigrationSourceResponse, error)
}

// NodeHookResponse is the response of a node hook. It is empty today, but
// KubeVirt's ExecuteNodeHookResponse can grow fields, and mapping them here
// keeps handler signatures stable.
type NodeHookResponse struct{}

// NodeHookHandler handles node hook invocations. Handlers must be idempotent
// and safe for concurrent use: virt-handler dispatches concurrent RPCs from
// its workers to one handler instance.
type NodeHookHandler interface {
	ExecuteNodeHook(ctx context.Context, req *NodeHookRequest) (*NodeHookResponse, error)
}
