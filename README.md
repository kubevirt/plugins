# kubevirt-plugins

Go SDK for writing [KubeVirt](https://kubevirt.io) plugins: hooks that customize virtual machines at defined points of their lifecycle without patching KubeVirt itself ([VEP 190](https://github.com/kubevirt/enhancements/tree/main/veps/sig-compute/190-kubevirt-structured-plugins)).
A plugin is a small Go program; the SDK handles the gRPC plumbing and generates the deployment manifests for it.

> **Alpha** - the plugin API (KubeVirt v1.9+) is alpha. Expect breaking changes.

## Compatibility

SDK `vX.Y` targets KubeVirt `vX.Y` and uses that release's `kubevirt.io/api`.

| SDK | KubeVirt | Go |
|-----|----------|----|
| v1.10.x | v1.10.x | >= 1.26 |
| v1.9.x | v1.9.x | >= 1.24 |

Build plugins with the SDK minor matching the KubeVirt minor of the target cluster, and pin it (e.g. `go get github.com/kubevirt/plugins@v1.10`).
The API is alpha: a new minor can break plugins without shims, so update with `go get -u=patch` or an explicit `@v1.Y`, never a plain `go get -u`.
See [RELEASING.md](RELEASING.md) for how releases are cut.

## Quick start

Requires a cluster running KubeVirt v1.10 with the `Plugins` feature gate enabled.
To enable it, add `Plugins` to `spec.configuration.developerConfiguration.featureGates` in the KubeVirt CR (for example with `kubectl edit kubevirt -n kubevirt kubevirt`).

A plugin registers handlers for hook points and calls `Execute`:

```go
package main

import (
	"context"

	"github.com/kubevirt/plugins/pkg/sdk/plugin"
)

type descriptionHook struct{}

// GuestDefinition can run several times per VM start and sees its own earlier
// output, so the handler assigns a fixed value instead of appending.
func (*descriptionHook) GuestDefinition(_ context.Context, req *plugin.GuestDefinitionRequest) error {
	req.Domain.Description = "injected"
	return nil
}

func main() {
	plugin.New("description-injector").
		WithImage("quay.io/myorg/description-injector:v0.1.0").
		WithLauncherHook(plugin.GuestDefinition(&descriptionHook{}).WithCondition(`"description-injector" in vmi.Labels && vmi.Labels["description-injector"] == "true"`)).
		WithSidecarPodLabels(map[string]string{"description-injector": "true"}).
		Execute()
}
```

Only VMIs labelled `description-injector=true` get the sidecar; see [Scoping sidecar injection](#scoping-sidecar-injection) for why the example scopes it (cluster-wide injection breaks dedicated-CPU VMIs, and an unpullable image blocks every VM).

The handler mutates `req.Domain` in place and returns nil; the SDK marshals the result.
`Execute` dispatches on the command line: `serve` starts the gRPC server (what the container runs), `generate` emits the deployment artifacts.

In a new directory with the code above in main.go:

```bash
go mod init example.com/description-injector
go get github.com/kubevirt/plugins@v1.10
go run . generate          # writes Dockerfile, Makefile and deploy/*.yaml
make push                  # builds and pushes the WithImage image
kubectl apply -f deploy/
```

`generate` writes a `Dockerfile` and a `Makefile` next to the plugin's source; the `Makefile` also has `build`, `test` and `vet` targets, using podman or docker.
The numbered manifests in `deploy/` contain the Plugin CR that registers the hooks with KubeVirt, plus, for sidecar hooks, a MutatingAdmissionPolicy and binding that inject the plugin's container into virt-launcher pods.
Plugins with node hooks also get a DaemonSet there, and RBAC when `WithRBACRules` is used.
The generated `Makefile` builds and pushes the `WithImage` image (default `quay.io/myorg/<name>:latest`), the same image the manifests reference, so replace `quay.io/myorg` with a registry you can push to.
To see it work, start a VMI labelled `description-injector=true` and look for `<description>injected</description>` in its domain XML:

```bash
kubectl exec -n <namespace> <launcher-pod> -c compute -- \
  virsh -c 'qemu+unix:///session?socket=/var/run/libvirt/virtqemud-sock' dumpxml <namespace>_<vmi-name>
```

Domain tweaks that need no Go can use a CEL expression instead.
KubeVirt evaluates it inside virt-launcher and no container is deployed, so `generate` emits only the Plugin CR (no Dockerfile, Makefile or admission policy):

```go
plugin.New("cpu-mode").
	WithLauncherHook(plugin.GuestDefinitionCEL(`Domain{CPU: DomainCPU{Mode: "host-passthrough"}}`)).
	Execute()
```

## Hook points

| Hook point | Runs | When | Can change |
|------------|------|------|------------|
| GuestDefinition | launcher | whenever the domain is defined: at VM start and on the migration target | the domain XML |
| PreBoot | launcher | after the boot artifacts were generated, before the guest starts | the boot artifacts (cloud-init ISO, ephemeral disks) |
| PreMigrationSource | launcher | on the migration source, just before libvirt is asked to migrate | the migration flags and parameters |
| PreVMStart | node | before each virt-handler sync while the VMI is not running yet | node state |
| PostVMStart | node | once, when the VMI phase changes to Running | node state |
| OnVMStop | node | before the VM is shut down, gracefully or forced | node state |
| PostVMStop | node | when the VMI is final and the domain is gone; may fire repeatedly | node state |
| PreMigrationSource | node | on the source node, just before the migration starts | node state |
| PreMigrationTarget | node | on the target node, while the migration target is prepared | node state |
| PostMigrationTarget | node | on the target node, after the migration was finalized | node state |

Launcher hooks run next to the VM: the generated admission policy injects the plugin's container as a sidecar into the virt-launcher pod and KubeVirt calls it over gRPC on a unix socket.
Node hooks run in a DaemonSet on every node, which virt-handler dials.
A plugin can mix both.

Node hooks are registered with the typed hook point constants:

```go
type nodeHook struct{}

func (*nodeHook) ExecuteNodeHook(_ context.Context, req *plugin.NodeHookRequest) (*plugin.NodeHookResponse, error) {
	log.Printf("%s: node %s handles vmi %s/%s", req.HookPoint, req.NodeName, req.VMI.Namespace, req.VMI.Name)
	return nil, nil
}

plugin.New("node-plugin").
	WithNodeHook(plugin.NodeHookPreVMStart, plugin.NodeHandler(&nodeHook{})).
	WithNodeHook(plugin.NodeHookPostVMStop, plugin.NodeHandler(&nodeHook{})).
	Execute()
```

A failing node hook fails the virt-handler sync, which is requeued and fires the hooks again, so handlers must be idempotent.
virt-handler also calls the hooks from several workers at once, so handlers must be safe for concurrent use.

A plugin, and each of its hooks, can carry a CEL condition evaluated against the VMI (`WithCondition`); a false condition skips the hook.
The only variable is `vmi`, addressed with Go struct field names (`vmi.Spec.Domain.CPU.Cores`).
Evaluation errors are hard failures regardless of the failure strategy, so guard map access: `"k" in vmi.Labels && vmi.Labels["k"] == "v"`.
The SDK validates conditions when the plugin is constructed, and `cel.EvaluateCondition` from `pkg/sdk/cel` evaluates one locally in unit tests.
A failing hook fails the VM start, migration or sync by default; `WithFailureStrategy(plugin.Ignore)` opts out, and `WithTimeout` bounds each call.

CEL expressions (GuestDefinition only) see `vmi` and `domainSpec` and must return a `Domain{...}` struct literal.
KubeVirt deep-merges it: only fields present in the literal are written, and lists are replaced wholesale, so rebuild a list from `domainSpec` to keep existing entries.

### PreBoot hooks

PreBoot rewrites the artifacts virt-launcher generated, for example to inject extra files into the cloud-init ISO.
It may repeat after a failed start and filesystem changes are never rolled back, so write idempotently and replace artifacts atomically.
`pkg/sdk/artifact` does the atomic swap (`ReplaceFile`, `ReplaceDir`) and preserves ownership and permissions; pass `ctx` to everything the hook does.
Keep the default Fail strategy here: a timed-out hook is not stopped, and with Ignore the VM starts while the hook keeps writing.
See [tests/pre_boot_artifact/plugin.go](tests/pre_boot_artifact/plugin.go).

### PreMigrationSource hooks

PreMigrationSource adjusts what KubeVirt passes to libvirt just before a migration.
The response carries only the changes: a nil field keeps the current value, `migration.Int32(2)` overrides it and `migration.RemoveInt32()` removes it; flags are set and cleared with separate masks.
A nil response changes nothing.
With the default Fail strategy, a hook error aborts the migration and the error appears in the VMI's `status.migrationState.failureReason`.
See [pkg/sdk/migration](pkg/sdk/migration) and [tests/pre_migration_source/plugin.go](tests/pre_migration_source/plugin.go).

## Scoping sidecar injection

While the `Plugins` feature gate is on, the generated admission policy injects a sidecar into every virt-launcher pod in the cluster.
Conditions do not scope this: they only decide whether the hooks are called, so a filtered-out VMI still gets the container.
An injected container without resource requests makes the launcher pod Burstable, which breaks Guaranteed QoS and dedicated-CPU VMIs.
A sidecar image that cannot be pulled keeps every VM from starting.

Scope the injection with `WithSidecarPodLabels` and set `WithSidecarResources`; KubeVirt copies VMI labels to the launcher pod, so labelling VMIs scopes the plugin to them.
Keep a hook condition on the same label, so an unlabelled VMI neither gets the sidecar nor waits for its socket:

```go
plugin.New("my-plugin").
	WithLauncherHook(plugin.GuestDefinition(&handler{}).WithCondition(`"my-plugin" in vmi.Labels && vmi.Labels["my-plugin"] == "true"`)).
	WithSidecarPodLabels(map[string]string{"my-plugin": "true"}).
	WithSidecarResources(corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("50m"),
			corev1.ResourceMemory: resource.MustParse("64Mi"),
		},
	}).
	Execute()
```

A PreBoot sidecar is additionally mounted into the ephemeral-disks volume of every pod the sidecar is injected into, including VMIs whose hook condition skips the hook.

## Examples

The plugins under `tests/` are working examples and double as the SDK's functional tests:

- [tests/guest_definition/plugin.go](tests/guest_definition/plugin.go) - a GuestDefinition sidecar hook, including a label-gated variant
- [tests/pre_boot/plugin.go](tests/pre_boot/plugin.go) - a PreBoot hook reading the artifact paths
- [tests/pre_boot_artifact/plugin.go](tests/pre_boot_artifact/plugin.go) - rewriting the cloud-init ISO atomically
- [tests/pre_migration_source/plugin.go](tests/pre_migration_source/plugin.go) - adjusting migration flags and parameters
- [tests/node_hook_lifecycle/plugin.go](tests/node_hook_lifecycle/plugin.go) - node hooks across the VM lifecycle
- [tests/mixed_hooks/plugin.go](tests/mixed_hooks/plugin.go) - launcher and node hooks in one plugin
- [tests/multi_entrypoint/plugin.go](tests/multi_entrypoint/plugin.go) - splitting hooks across several containers

Entrypoints split a plugin's hooks across multiple sidecar containers or DaemonSets (`WithEntrypoint`); each hook point can be registered once per entrypoint, so compose transformations in one handler instead.
`generate` emits one DaemonSet and admission policy per entrypoint, and each container serves only its subset.
Plugin images must keep `ENTRYPOINT ["<binary>", "serve"]` like the generated `Dockerfile`, since the manifests pass their flags as container `args`.

## Development

```bash
make test          # unit tests (pkg/...)
make vet
make cluster-up    # local kubevirtci cluster
make cluster-sync  # deploy KubeVirt (with the Plugins feature gate) and build the test plugin images
make functest      # functional tests in tests/ against the cluster
```

The test and vet targets run through a containerized Go toolchain, so no local Go is required.

## Known limitation

When a GuestDefinition hook is selected for a VMI, virt-launcher converts the domain to `libvirtxml` and back.
Go's XML decoder silently skips elements and attributes that libvirtxml does not model, so a hook cannot introduce domain elements outside KubeVirt's vendored `libvirtxml` (v1.12007.0 in KubeVirt v1.10) - for CEL and sidecar hooks alike.
Supporting a new element needs libvirtxml support upstream and a KubeVirt release that vendors it.

API details are in godoc: [pkg/sdk/plugin](https://pkg.go.dev/github.com/kubevirt/plugins/pkg/sdk/plugin), [pkg/sdk/artifact](https://pkg.go.dev/github.com/kubevirt/plugins/pkg/sdk/artifact), [pkg/sdk/cel](https://pkg.go.dev/github.com/kubevirt/plugins/pkg/sdk/cel) and [pkg/sdk/migration](https://pkg.go.dev/github.com/kubevirt/plugins/pkg/sdk/migration).
