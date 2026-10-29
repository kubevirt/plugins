# kubevirt-plugins

SDK and catalog for building [KubeVirt](https://kubevirt.io) plugins ([VEP 190](https://github.com/kubevirt/enhancements/tree/main/veps/sig-compute/190-kubevirt-structured-plugins)).

> **Alpha** - The KubeVirt plugin API is alpha (v1.9+). Expect breaking changes.

## Overview

This repo provides:

- **SDK** (`pkg/sdk/plugin`) - A Go framework for developing launcher hooks and node hooks with minimal boilerplate.
- **Artifact helpers** (`pkg/sdk/artifact`) - Atomic replacement of boot artifacts for PreBoot hooks.
- **Catalog** (`plugins/`) - A collection of community plugins (coming soon).

## Compatibility

SDK `vX.Y.Z` targets KubeVirt `vX.Y` and uses that release's `kubevirt.io/api` and vendored libvirtxml.

| SDK | KubeVirt | kubevirt.io/api | libvirtxml | Go |
|-----|----------|-----------------|------------|----|
| v1.10.x | v1.10.x | v1.10.0-beta.0.0.20261029030351-f99bef8ca6b0[^api-v110] | v1.12002.0 | >= 1.26 |
| v1.9.x | v1.9.x | v1.9.1 | v1.12002.0 | >= 1.24 |

- Build plugins with the SDK minor matching the KubeVirt minor of the target
  cluster, and pin it (e.g. `go get github.com/kubevirt/plugins@v1.10`).
- The plugin API is alpha: a new minor can break plugins (renamed hook points,
  changed handler signatures) without compatibility shims. Patch releases only
  carry fixes and dependency alignment.
- Go tooling and dependency bots treat a new `v1` minor as compatible, so
  update with `go get -u=patch` or an explicit `@v1.Y`, never a plain
  `go get -u`, and limit Renovate or Dependabot to patch updates. Moving to a
  new SDK minor means moving to the matching KubeVirt minor.
- `main` follows KubeVirt's development branch and may not work with any
  released KubeVirt.

[^api-v110]: A main pseudo-version rather than the beta.0 tag: no v1.10
    release tag carries the new hook points yet. It is verified
    byte-identical to KubeVirt release-1.10 for core/v1 and
    plugin/v1alpha1, and is re-pinned to the v1.10.0 tag at GA.

See [RELEASING.md](RELEASING.md) for how releases are cut.

## Example: SMBIOS Manufacturer Injector

A GuestDefinition hook that sets a custom SMBIOS manufacturer based on the VMI
name whenever virt-launcher defines the domain.

```go
package main

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/kubevirt/plugins/pkg/sdk/plugin"
	"libvirt.org/go/libvirtxml"
)

type smbiosHandler struct{}

// GuestDefinition can run 2-3 times per VM start and sees its own earlier
// output, so the handler assigns fixed values instead of appending.
func (*smbiosHandler) GuestDefinition(_ context.Context, req *plugin.GuestDefinitionRequest) error {
	if req.Domain.OS == nil {
		req.Domain.OS = &libvirtxml.DomainOS{}
	}
	req.Domain.OS.SMBios = &libvirtxml.DomainSMBios{Mode: "sysinfo"}
	req.Domain.SysInfo = []libvirtxml.DomainSysInfo{{
		SMBIOS: &libvirtxml.DomainSysInfoSMBIOS{
			System: &libvirtxml.DomainSysInfoSystem{
				Entry: []libvirtxml.DomainSysInfoEntry{
					{Name: "manufacturer", Value: "KubeVirt-" + req.VMI.Name},
				},
			},
		},
	}}
	return nil
}

func main() {
	p := plugin.New("smbios-injector").
		WithLauncherHook(
			plugin.GuestDefinition(&smbiosHandler{}).
				WithCondition(`"smbios-injector" in vmi.Labels`).
				WithFailureStrategy(plugin.Fail),
		).
		WithSidecarPodLabels(map[string]string{"smbios-injector": "true"}).
		WithSidecarResources(corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("50m"),
				corev1.ResourceMemory: resource.MustParse("64Mi"),
			},
		})

	p.Execute() // "serve" starts the gRPC server; "generate" emits deployment artifacts
}
```

The condition only decides whether the hook is *called* for a VMI; the
injection of the sidecar container itself is scoped by
`WithSidecarPodLabels`, and `WithSidecarResources` keeps the launcher pod's
quality of service intact - see [Sidecar injection scope](#sidecar-injection-scope).

Domain tweaks that need no Go can use a CEL expression instead. KubeVirt
evaluates it inside virt-launcher and no container is deployed:

```go
p := plugin.New("smbios-injector").
	WithLauncherHook(plugin.GuestDefinitionCEL(`Domain{Title: "injected"}`))
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

Running `generate` produces build files in the project root and numbered YAML
manifests in `deploy/`:

```
Dockerfile                                    # Multi-stage build (distroless runtime)
Makefile                                      # build/push targets using podman or docker
deploy/
  01-plugin.yaml                              # Plugin CR - registers hooks with KubeVirt
  02-mutating-admission-policy.yaml           # MAP - injects the sidecar into virt-launcher pods
  03-mutating-admission-policy-binding.yaml   # MAP binding - applies the policy to all pods (no paramRef or matchResources)
```

The manifests are numbered in emission order: the RBAC bundle (only for node
hook plugins that configure RBAC rules), one DaemonSet per node entrypoint,
the Plugin CR, then one MutatingAdmissionPolicy and binding per sidecar
entrypoint. Named entrypoints suffix their files (`gpu-daemonset.yaml`,
`gpu-mutating-admission-policy.yaml`).

The Plugin CR is marshalled from typed `kubevirt.io/api/plugin` objects, so
its field names and enum values match the CRD by construction; it references
the generated policies under `spec.mutatingAdmissionPolicies`. Build files
are written only when the plugin serves hooks from a container - CEL-only
plugins are evaluated by KubeVirt directly and need no image.

## Sidecar injection scope

While the Plugins feature gate is on, the generated MutatingAdmissionPolicy
injects a sidecar plugin's container into **every** virt-launcher pod in the
cluster. Plugin and hook conditions do not scope the injection: they only
decide whether the hooks are *called* for a VMI. A VMI whose condition
filters the hook out still gets the container, and the container always
runs - it just never receives a hook call.

This has three consequences for every VM in the cluster, selected or not:

- **Quality of service.** An injected container without resource requests
  makes the virt-launcher pod Burstable, so Guaranteed pods become
  impossible and CPU-pinned (dedicated-CPU) VMIs fail to start with
  "failed to guarantee pod resources".
- **Artifact exposure.** A sidecar serving PreBoot mounts the
  `ephemeral-disks` volume of every VM read-write, including the cloud-init
  ISO with its user-data - not only of the VMIs the plugin targets.
- **Availability.** A sidecar image that cannot be pulled keeps every
  virt-launcher pod from becoming Ready, so no VM in the cluster starts.

Scope the plugin with pod labels and keep the launcher pods' quality of
service:

```go
p := plugin.New("my-plugin").
	WithLauncherHook(plugin.GuestDefinition(&handler{}).WithCondition(`"my-plugin" in vmi.Labels`)).
	WithSidecarPodLabels(map[string]string{"my-plugin": "true"}).
	WithSidecarResources(corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("50m"),
			corev1.ResourceMemory: resource.MustParse("64Mi"),
		},
	})
```

`WithSidecarPodLabels` adds match conditions to the generated policy, so the
sidecar lands only in virt-launcher pods carrying these labels; KubeVirt
copies VMI labels to the launcher pod, so labelling VMIs scopes the plugin
to them. Keep a hook condition on the same label: a VMI without the label
then neither gets the sidecar nor waits for its socket.
`WithSidecarResources` renders the requests and limits into the injected
container, keeping Guaranteed QoS and dedicated-CPU VMIs working.

## Hook Points

Hooks run in two places. Launcher hooks are invoked by virt-launcher, the pod
that runs the VM: CEL hooks are evaluated inside virt-launcher itself, sidecar
hooks by a container injected into the virt-launcher pod. Node hooks are
invoked by virt-handler, which dials a unix socket served by the plugin's
DaemonSet on the target node.

Plugins are selected per VMI: KubeVirt evaluates the plugin-level condition
and then each hook's condition against the VMI, and runs the selected hooks
sequentially - plugins in name order, hooks in declaration order. Each
launcher hook's output feeds the next one. The default failure strategy is
Fail; every hook call is bounded by its timeout (30s by default,
`WithTimeout` to override), and the sidecars of one invocation share a single
30s socket-readiness budget.

### Launcher hooks

| Hook point | CEL | Runs | Request | Response |
|------------|-----|------|---------|----------|
| GuestDefinition | yes | whenever the domain is defined: at VM start and on the migration target | `Domain`, `VMI`, `InvocationContext` (`Boot` or `MigrationTarget`) | the mutated domain |
| PreBoot | no | after the boot artifacts were generated, before the guest starts | `VMI`, artifact paths | empty; only the error matters |
| PreMigrationSource | no | on the migration source, just before libvirt is asked to migrate | `VMI`, migration flags and parameters | changes only |

Notes:

- GuestDefinition can run 2-3 times per VM start: to allocate PCIe hotplug
  ports, virt-launcher defines the domain several times, and later rounds see
  the earlier hooks' output. Handlers must be idempotent - assign fixed values
  instead of appending.
- A GuestDefinition response must be a complete domain; an empty one is a
  failure, not "no change". SDK handlers just mutate `req.Domain` in place and
  return nil; the SDK marshals the result.
- PreBoot can repeat after a failed start, and filesystem changes are never
  rolled back. See Writing PreBoot Hooks below.
- PreMigrationSource responses carry only the changes to apply; a nil
  response changes nothing. See Adjusting Migrations with PreMigrationSource
  below.

### Node hooks

| Hook point | Runs |
|------------|------|
| PreVMStart | before each virt-handler sync while the VMI is not running yet |
| PostVMStart | once, when the VMI phase changes to Running |
| OnVMStop | before the VM is shut down, gracefully or forced |
| PostVMStop | when the VMI is final and the domain is gone; may fire repeatedly |
| PreMigrationSource | on the source node, just before the migration starts |
| PreMigrationTarget | on the target node, while the migration target is prepared |
| PostMigrationTarget | on the target node, after the migration was finalized |

The request carries the hook point, the VMI and the node name; the response is
empty and only the gRPC status matters. A failing hook fails the virt-handler
sync, which is requeued with backoff and fires the hooks again - node hooks
must be idempotent. Handlers must also be safe for concurrent use:
virt-handler dispatches concurrent RPCs from its workers to one handler
instance.

Register node hooks with the typed hook point constants:

```go
type prepareHook struct{}

func (*prepareHook) ExecuteNodeHook(_ context.Context, req *plugin.NodeHookRequest) (*plugin.NodeHookResponse, error) {
	log.Printf("%s: node %s prepares for vmi %s/%s", req.HookPoint, req.NodeName, req.VMI.Namespace, req.VMI.Name)
	return nil, nil
}

p := plugin.New("my-plugin").
	WithNodeHook(plugin.NodeHookPreVMStart, plugin.NodeHandler(&prepareHook{}).WithFailureStrategy(plugin.Fail)).
	WithNodeHook(plugin.NodeHookPostVMStop, plugin.NodeHandler(&cleanupHook{}))
```

`generate` produces the DaemonSet (one per entrypoint), which serves its
socket under `/var/run/kubevirt/plugins`, a host directory mounted into
virt-handler.

## Conditions

Both plugins and individual hooks can carry a CEL condition, evaluated against
the VMI before the hook runs; a false condition skips the hook.

- The only variable is `vmi`, addressed with Go struct field names:
  `vmi.Labels`, `vmi.Annotations`, `vmi.Spec.Domain.CPU.Cores`. JSON-style
  access (`vmi.metadata.name`) does not compile.
- The expression must return bool. An empty condition always matches.
- `domainSpec` is not available in conditions; it only exists inside
  GuestDefinition CEL expressions.
- Evaluation errors are hard failures regardless of the failure strategy,
  unlike hook errors. Guard map access:
  `"k" in vmi.Labels && vmi.Labels["k"] == "v"`.

The SDK validates conditions when the plugin is constructed: `WithCondition`
panics on an expression that does not compile. `cel.EvaluateCondition(expr, vmi)`
from `pkg/sdk/cel` evaluates a condition locally, for unit tests.

## GuestDefinition CEL Expressions

A CEL GuestDefinition hook needs no container: virt-launcher evaluates the
expression itself. CEL is only supported for the GuestDefinition hook point.
The expression sees two variables, `vmi` and `domainSpec` (the current
domain), and must produce a `Domain{...}` struct literal:

```go
plugin.GuestDefinitionCEL(`Domain{CPU: DomainCPU{Mode: "host-passthrough"}}`)
```

KubeVirt deep-merges the literal into the domain:

- Only fields present in the literal are written; everything else is
  untouched, at every nesting level.
- Lists are replaced wholesale. To keep existing entries, rebuild the whole
  list, e.g. from `domainSpec`.
- `Domain{}` is a valid no-op; an explicit `null` sets the zero value.
- Use `uint(...)` for Go `uint` fields, e.g. `DomainMemory{Value: uint(2048)}`.

Validation checks the static type only: an identity expression like
`domainSpec` passes validation but fails at runtime on every VM, because the
result must be a struct literal, not the input domain. CEL hooks do not
support entrypoints or timeouts (`WithEntrypoint` and `WithTimeout` panic),
since KubeVirt evaluates them without a container.

## Writing PreBoot Hooks

PreBoot runs after virt-launcher generated the boot artifacts (the cloud-init
ISO, the ephemeral disk directory) and before the guest starts, and can
rewrite them - for example to inject extra files into the ISO. Three
properties of the hook point shape how it must be written:

- It may repeat after a failed start, and filesystem changes are never rolled
  back. Write idempotently: fixed file names and contents, not appends.
- A hook that exceeds its timeout is not stopped. With the Ignore strategy
  KubeVirt proceeds to start the VM while the hook keeps writing - prefer
  `WithFailureStrategy(plugin.Fail)` for hooks with filesystem side effects,
  and use `Ignore` only when the change is best effort and a missing or late
  change is safe.
- Write transactionally and honor the context: pass `ctx` to everything the
  hook does, so it stops when the deadline expires, and replace artifacts
  atomically instead of editing them in place, so an interrupted run never
  leaves a partial artifact behind.

`pkg/sdk/artifact` replaces an artifact atomically - `ReplaceFile` for the
cloud-init ISO, `ReplaceDir` for subdirectories of the ephemeral disk
directory:

```go
type isoPatchHook struct{}

func (*isoPatchHook) PreBoot(ctx context.Context, req *plugin.PreBootRequest) (*plugin.PreBootResponse, error) {
	isoPath, ok := req.CloudInitISO()
	if !ok {
		return nil, nil // no cloud-init data for this VMI
	}

	// ReplaceFile copies the ISO to a temporary sibling, runs the function
	// on the copy, and swaps it in atomically.
	return nil, artifact.ReplaceFile(ctx, isoPath, func(tmpPath string) error {
		return patchISO(ctx, tmpPath) // e.g. inject extra user data
	})
}
```

`ReplaceFile` and `ReplaceDir` copy the artifact to a temporary sibling, run
the given function on the copy and swap it in atomically, so virt-launcher,
QEMU and later hook invocations see either the complete old or the complete
new version. They restore ownership and permissions and leave the original
untouched on any error.

The helpers need no extra privileges when the sidecar runs as the VM's
non-root user (the default). A root sidecar needs `CAP_CHOWN` and
`CAP_FOWNER`, and should chown the entries it creates to the qemu user (107)
when QEMU needs to reach them.

The artifacts live in the `ephemeral-disks` volume, which virt-launcher mounts
at `/var/run/kubevirt-ephemeral-disks`; the files are owned by qemu (UID and
GID 107). A PreBoot sidecar must see them at the same path and as the same
user: the MAP generated for an entrypoint that serves PreBoot mounts the
volume at `/var/run/kubevirt-ephemeral-disks` and runs the sidecar as 107.
`req.CloudInitISO()` and `req.EphemeralDiskDir()` return the artifact paths
present for this VMI.

## Adjusting Migrations with PreMigrationSource

PreMigrationSource runs on the migration source just before libvirt is asked
to migrate, and can adjust the flags and parameters KubeVirt passes to
libvirt. The request carries the full configuration; the response carries
only the changes:

```go
type parallelHook struct{}

func (*parallelHook) PreMigrationSource(_ context.Context, req *plugin.PreMigrationSourceRequest) (*plugin.PreMigrationSourceResponse, error) {
	if req.Libvirt == nil || !req.Libvirt.Flags.Has(migration.Live) {
		return nil, nil // not a live migration: change nothing
	}
	return &plugin.PreMigrationSourceResponse{Libvirt: &plugin.LibvirtMigrationChanges{
		SetFlags: migration.Parallel,
		Params: &plugin.LibvirtDomainMigrateParameters{
			ParallelConnections: migration.Int32(2),
		},
	}}, nil
}
```

- Read the request with `req.Libvirt.Flags.Has(migration.PostCopy)` and the
  `migration.StringValue` / `Uint64Value` / `Int32Value` / `StringsValue`
  helpers, which also report whether a parameter is present.
- A nil response, or one with a nil `Libvirt`, changes nothing.
- Parameters are tri-state: a nil field keeps the current value,
  `migration.Int32(2)` overrides it (zero values included) and
  `migration.RemoveInt32()` removes it. List parameters use
  `migration.Strings(...)` to override and `migration.RemoveStrings()` to
  remove.
- Flags merge as `(flags | SetFlags) &^ ClearFlags`; a bit present in both
  masks is an error, which the SDK rejects before KubeVirt does.
- A Fail failure aborts the migration, with the error text in the VMI's
  `MigrationState.FailureReason`; an ignored failure keeps the previous
  configuration.

## Plugin Types

| Type | Description | Deployment |
|------|-------------|------------|
| CEL GuestDefinition hook | Inline CEL expression producing a sparse `Domain` | Plugin CR only (no container) |
| Sidecar launcher hook | gRPC sidecar running GuestDefinition, PreBoot or PreMigrationSource | Sidecar injected into virt-launcher pods via MutatingAdmissionPolicy |
| Node hook | gRPC DaemonSet invoked at VM lifecycle points | DaemonSet + Plugin CR |

## Multi-Container Plugins

Plugins that need separate containers for different concerns can use
entrypoints to split hooks across multiple sidecar containers or DaemonSets.
Each sidecar hook point can be registered once per entrypoint - compose
multiple transformations in a single Go handler instead. Multiple
GuestDefinition CEL hooks are valid, though: they become separate CR entries
whose expressions KubeVirt evaluates in sequence.

```go
p := plugin.New("my-plugin").
	WithLauncherHook(
		plugin.GuestDefinition(&gpuHandler{}).WithEntrypoint("gpu-sidecar"),
	).
	WithNodeHook(plugin.NodeHookPreVMStart,
		plugin.NodeHandler(&sriovHandler{}).WithEntrypoint("sriov-daemon"),
	).
	WithNodeHook(plugin.NodeHookPostVMStop,
		plugin.NodeHandler(&cleanupHandler{}).WithEntrypoint("sriov-daemon"),
	)
```

Running `generate` produces per-entrypoint DaemonSets and
MutatingAdmissionPolicies. Each container runs
`serve --entrypoint <name> --role sidecar|daemonset` to serve only its subset
of hooks: launcher hooks in sidecars, node hooks in DaemonSets. Without
`--role`, `serve` starts whatever the entrypoint has and fails when it has
both kinds. CEL hooks have no entrypoint - KubeVirt evaluates them without a
container.

The manifests pass these flags as container `args`, which override the image's
`CMD`, so plugin images must use `ENTRYPOINT ["<binary>", "serve"]`, like the
generated `Dockerfile`.

## Limitations

### Domain elements unknown to libvirtxml are dropped

When a GuestDefinition hook is selected for a VMI, virt-launcher round-trips
the domain through
[libvirtxml](https://pkg.go.dev/libvirt.org/go/libvirtxml):
it converts KubeVirt's domain to a `libvirtxml.Domain`
([KubeVirt release-1.10 `translate.go`](https://github.com/kubevirt/kubevirt/blob/6f56294e5da821198cee50ab80933ec54b420c3e/pkg/virt-launcher/virtwrap/converter/translate/translate.go#L38-L51)),
runs the hooks on it - CEL expressions directly, sidecars over gRPC with XML
marshalled from it - parses every sidecar response back into a
`libvirtxml.Domain`
([KubeVirt release-1.10 `guest_definition.go`](https://github.com/kubevirt/kubevirt/blob/6f56294e5da821198cee50ab80933ec54b420c3e/pkg/virt-launcher/virtwrap/plugins/guest_definition.go#L44-L96))
and defines the VM from the re-marshalled result. Go's XML decoder silently
skips elements and attributes that libvirtxml does not model, so only what
KubeVirt's vendored libvirtxml models survives (v1.12002.0 in KubeVirt
v1.10). This happens inside virt-launcher, for CEL and sidecar hooks alike;
nothing a plugin or the SDK does can preserve such an element.

In KubeVirt v1.10 the round-tripped XML only reaches the domain definition
when a GuestDefinition hook is selected for the VMI. When none is selected,
the launcher keeps the XML produced by the legacy hook mechanism
([KubeVirt release-1.10 `libvirt_helper.go`](https://github.com/kubevirt/kubevirt/blob/6f56294e5da821198cee50ab80933ec54b420c3e/pkg/virt-launcher/virtwrap/util/libvirt_helper.go#L166-L187)).
On the migration target only the conversion back to KubeVirt's domain spec is
kept - the target does not define the domain from the hook result; the
migrated domain comes from the source's destination XML, which includes the
boot-time changes
([KubeVirt release-1.10 `live-migration-target.go`](https://github.com/kubevirt/kubevirt/blob/6f56294e5da821198cee50ab80933ec54b420c3e/pkg/virt-launcher/virtwrap/live-migration-target.go#L211-L223)).

The SDK parses the domain with its own libvirtxml dependency and requires the
version KubeVirt v1.10 vendors, so plugins built with it use at least that
version. A newer one does not help, since virt-launcher still drops what its
own version does not model.

Supporting a new domain element therefore needs libvirtxml support upstream
([libvirt-go-xml-module](https://gitlab.com/libvirt/libvirt-go-xml-module)) and
a KubeVirt release that vendors it.

## Project Structure

```
api/                  Vendored proto/gRPC types from kubevirt
pkg/sdk/artifact/     Atomic artifact replacement for PreBoot hooks
pkg/sdk/cel/          CEL condition and expression validation
pkg/sdk/migration/    Migration flag and parameter helpers
pkg/sdk/plugin/       SDK framework
pkg/sdk/testing/      Test fixtures and helpers
plugins/              Plugin catalog (coming soon)
```
