# kubevirt-plugins

SDK and catalog for building [KubeVirt](https://kubevirt.io) plugins ([VEP 190](https://github.com/kubevirt/enhancements/tree/main/veps/sig-compute/190-kubevirt-structured-plugins)).

> **Alpha** - The KubeVirt plugin API is alpha (v1.9+). Expect breaking changes.

## Overview

This repo provides:

- **SDK** (`pkg/sdk/plugin`) - A Go framework for developing domain hooks and node hooks with minimal boilerplate.
- **Catalog** (`plugins/`) - A collection of community plugins (coming soon).

## Example: SMBIOS Manufacturer Injector

A domain hook plugin that sets a custom SMBIOS manufacturer based on the VMI name.

```go
package main

import (
	"context"
	"fmt"

	v1 "kubevirt.io/api/core/v1"
	"libvirt.org/go/libvirtxml"

	"github.com/iholder101/kubevirt-plugins/pkg/sdk/plugin"
)

type smbiosHandler struct{}

func (h *smbiosHandler) MutateDomain(
	ctx context.Context,
	domain *libvirtxml.Domain,
	vmi *v1.VirtualMachineInstance,
) error {
	if domain.OS == nil {
		domain.OS = &libvirtxml.DomainOS{}
	}
	domain.OS.SMBios = &libvirtxml.DomainSMBios{
		Mode: "sysinfo",
	}
	domain.SysInfo = append(domain.SysInfo, libvirtxml.DomainSysInfo{
		Type: "smbios",
		System: &libvirtxml.DomainSysInfoSystem{
			Entry: []libvirtxml.DomainSysInfoEntry{
				{Name: "manufacturer", Value: fmt.Sprintf("KubeVirt-%s", vmi.Name)},
			},
		},
	})
	return nil
}

func main() {
	p := plugin.New("smbios-injector").
		WithDomainHook(
			plugin.ForLibvirt(&smbiosHandler{}).
				WithCondition(`has(object.spec.domain.firmware)`).
				WithFailureStrategy(plugin.Fail),
		)

	p.Execute() // "serve" starts gRPC server; "generate" emits deployment artifacts
}
```

### Build and Deploy

```bash
# Generate deployment artifacts
./smbios-injector generate

# Build and push the container image
make push REGISTRY=quay.io/myorg TAG=v0.1.0

# Deploy to the cluster
kubectl apply -f deploy/
```

### Generated Files

Running `generate` produces build files in the project root and YAML manifests in `deploy/`:

```
Dockerfile                                    # Multi-stage build (distroless runtime)
Makefile                                      # build/push targets using podman or docker
deploy/
  01-plugin.yaml                              # Plugin CR - registers hooks with KubeVirt
  02-mutating-admission-policy.yaml           # MAP - triggers sidecar injection on VMI creation
  03-mutating-admission-policy-binding.yaml   # MAP binding - binds the policy to the Plugin CR
```

For node hook plugins, `generate` also produces `01-daemonset.yaml` (and `01-rbac.yaml` if RBAC rules are configured).

## Plugin Types

| Type | Description | Deployment |
|------|-------------|------------|
| CEL domain hook | Inline CEL expressions modifying domain XML | Plugin CR only (YAML) |
| Sidecar domain hook | gRPC sidecar mutating domain XML | Sidecar injected via MutatingAdmissionPolicy |
| Node hook | gRPC DaemonSet invoked at VM lifecycle points | DaemonSet + Plugin CR |

## Multi-Container Plugins

Plugins that need separate containers for different concerns can use entrypoints to split
hooks across multiple DaemonSets or sidecar containers:

```go
p := plugin.New("my-plugin").
    WithDomainHook(
        plugin.ForLibvirt(&gpuHandler{}).WithEntrypoint("gpu-sidecar"),
    ).
    WithNodeHook(plugin.PreVMStart,
        plugin.NodeHandler(&sriovHandler{}).WithEntrypoint("sriov-daemon"),
    ).
    WithNodeHook(plugin.PostVMStop,
        plugin.NodeHandler(&cleanupHandler{}).WithEntrypoint("sriov-daemon"),
    )
```

Running `generate` produces per-entrypoint DaemonSets and MutatingAdmissionPolicies.
Each container runs with `--entrypoint <name>` to serve only its subset of hooks.

## Project Structure

```
api/               Vendored proto/gRPC types from kubevirt
pkg/sdk/plugin/    SDK framework
pkg/sdk/testing/   Test fixtures and helpers
plugins/           Plugin catalog (coming soon)
```
