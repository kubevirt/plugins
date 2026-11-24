package plugin

import (
	"path/filepath"

	pb "github.com/kubevirt/plugins/api/hooks/v1alpha1"
	v1 "kubevirt.io/api/core/v1"
	pluginv1alpha1 "kubevirt.io/api/plugin/v1alpha1"
)

// NodeHookRequest contains the inputs passed to a node hook handler.
type NodeHookRequest struct {
	// HookPoint is the lifecycle point being invoked (e.g. PreVMStart, OnVMStop).
	HookPoint NodeHookPoint
	// VMI is the VirtualMachineInstance associated with this hook invocation.
	VMI *v1.VirtualMachineInstance
	// NodeName is the Kubernetes node where the VM is running.
	NodeName string
}

// FailureStrategy controls how KubeVirt handles a plugin hook failure.
type FailureStrategy = pluginv1alpha1.FailureStrategy

const (
	Fail   = pluginv1alpha1.FailureStrategyFail
	Ignore = pluginv1alpha1.FailureStrategyIgnore
)

// LauncherHookPoint identifies a point in the VM lifecycle where a launcher hook can run.
type LauncherHookPoint = pluginv1alpha1.LauncherHookPoint

const (
	LauncherHookGuestDefinition    = pluginv1alpha1.LauncherHookGuestDefinition
	LauncherHookPreBoot            = pluginv1alpha1.LauncherHookPreBoot
	LauncherHookPreMigrationSource = pluginv1alpha1.LauncherHookPreMigrationSource
)

// InvocationContext identifies why a launcher hook is being invoked.
type InvocationContext = pluginv1alpha1.InvocationContext

const (
	InvocationContextBoot = pluginv1alpha1.InvocationContextBoot
	// InvocationContextMigrationSource is never sent to GuestDefinition by
	// KubeVirt v1.10 (only Boot and MigrationTarget are); migration-source
	// work belongs to the PreMigrationSource hook.
	InvocationContextMigrationSource = pluginv1alpha1.InvocationContextMigrationSource
	InvocationContextMigrationTarget = pluginv1alpha1.InvocationContextMigrationTarget
)

// PreBoot artifact path keys, as populated by virt-launcher.
const (
	PreBootArtifactCloudInitISO     = pb.PreBootArtifactCloudInitISO
	PreBootArtifactEphemeralDiskDir = pb.PreBootArtifactEphemeralDiskDir
)

const (
	// launcherSocketBasePath is the base directory for launcher hook sockets.
	launcherSocketBasePath = "/var/run/kubevirt-plugin"
	// nodeSocketBasePath is the base directory for node hook sockets.
	nodeSocketBasePath = "/var/run/kubevirt/plugins"
	// pluginSocketsVolumeName is the virt-launcher pod volume the plugin sockets live in.
	pluginSocketsVolumeName = "kubevirt-plugin-sockets"
	// ephemeralDisksVolumeName is the virt-launcher pod volume holding the PreBoot artifacts.
	ephemeralDisksVolumeName = "ephemeral-disks"
	// ephemeralDisksMountPath is where virt-launcher mounts the ephemeral
	// disks; a PreBoot sidecar must mount the volume at the same path to see
	// the artifacts.
	ephemeralDisksMountPath = "/var/run/kubevirt-ephemeral-disks"
	// sidecarUID and sidecarGID identify the qemu user virt-launcher owns the
	// boot artifacts with; a PreBoot sidecar needs them to read and rewrite
	// the artifacts.
	sidecarUID = 107
	sidecarGID = 107
)

// LauncherSocketPath returns the socket path a launcher hook sidecar serves
// for pluginName. The entrypoint distinguishes sidecars of the same plugin;
// when it is empty or equals the plugin name the default path is used.
func LauncherSocketPath(pluginName, entrypoint string) string {
	if entrypoint == "" || entrypoint == pluginName {
		return filepath.Join(launcherSocketBasePath, pluginName, "launcher.sock")
	}
	return filepath.Join(launcherSocketBasePath, pluginName, entrypoint, "launcher.sock")
}

// NodeSocketPath returns the socket path a node hook DaemonSet serves for
// pluginName. The entrypoint distinguishes DaemonSets of the same plugin;
// when it is empty or equals the plugin name the default path is used.
func NodeSocketPath(pluginName, entrypoint string) string {
	if entrypoint == "" || entrypoint == pluginName {
		return filepath.Join(nodeSocketBasePath, pluginName, "node.sock")
	}
	return filepath.Join(nodeSocketBasePath, pluginName, entrypoint, "node.sock")
}
