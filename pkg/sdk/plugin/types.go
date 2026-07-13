package plugin

import (
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	v1 "kubevirt.io/api/core/v1"
)

// NodeHookRequest contains the inputs passed to a node hook handler.
type NodeHookRequest struct {
	// HookPoint is the lifecycle point being invoked (e.g. PreVMStart, PostVMStop).
	HookPoint string
	// VMI is the VirtualMachineInstance associated with this hook invocation.
	VMI *v1.VirtualMachineInstance
	// NodeName is the Kubernetes node where the VM is running.
	NodeName string
}

// FailureStrategy controls how KubeVirt handles a plugin hook failure.
type FailureStrategy string

const (
	Fail   FailureStrategy = "Fail"
	Ignore FailureStrategy = "Ignore"
)

// SidecarConfig defines the container and volumes injected alongside virt-launcher for a domain hook.
// It will be used when MutatingAdmissionPolicy-based sidecar generation is fully implemented.
type SidecarConfig struct {
	// Container is the sidecar container spec injected into the virt-launcher pod.
	Container corev1.Container
	// Volumes are additional volumes mounted into the virt-launcher pod.
	Volumes []corev1.Volume
}

const (
	// DomainSocketBasePath is the base directory for domain hook sockets.
	DomainSocketBasePath = "/var/run/kubevirt-plugin"
	// NodeSocketBasePath is the base directory for node hook sockets.
	NodeSocketBasePath = "/var/run/kubevirt/plugins"
)

// DomainSocketPath returns the full socket path for a domain hook plugin.
func DomainSocketPath(pluginName string) string {
	return filepath.Join(DomainSocketBasePath, pluginName, "domain.sock")
}

// NodeSocketPath returns the full socket path for a node hook plugin.
func NodeSocketPath(pluginName string) string {
	return filepath.Join(NodeSocketBasePath, pluginName, "node.sock")
}

// DomainSocketPathForEntrypoint returns the socket path for a domain hook
// with a specific entrypoint. When the entrypoint equals the plugin name
// or is empty, the default path is used for backward compatibility.
func DomainSocketPathForEntrypoint(pluginName, entrypoint string) string {
	if entrypoint == "" || entrypoint == pluginName {
		return DomainSocketPath(pluginName)
	}
	return filepath.Join(DomainSocketBasePath, pluginName, entrypoint, "domain.sock")
}

// NodeSocketPathForEntrypoint returns the socket path for a node hook
// with a specific entrypoint. When the entrypoint equals the plugin name
// or is empty, the default path is used for backward compatibility.
func NodeSocketPathForEntrypoint(pluginName, entrypoint string) string {
	if entrypoint == "" || entrypoint == pluginName {
		return NodeSocketPath(pluginName)
	}
	return filepath.Join(NodeSocketBasePath, pluginName, entrypoint, "node.sock")
}
