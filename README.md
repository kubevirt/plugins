# kubevirt-plugins

| nightly | latest release [![latest SDK release](https://img.shields.io/github/v/tag/kubevirt/plugins?sort=semver&label=)](https://github.com/kubevirt/plugins/releases/latest) | previous release | proto drift |
|:-:|:-:|:-:|:-:|
| [![nightly](https://prow.ci.kubevirt.io/badge.svg?jobs=periodic-kubevirt-plugins-e2e-nightly)](https://testgrid.k8s.io/kubevirt-plugins-periodics#periodic-kubevirt-plugins-e2e-nightly) | [![latest release](https://prow.ci.kubevirt.io/badge.svg?jobs=periodic-kubevirt-plugins-e2e-latest-release)](https://testgrid.k8s.io/kubevirt-plugins-periodics#periodic-kubevirt-plugins-e2e-latest-release) | [![previous release](https://prow.ci.kubevirt.io/badge.svg?jobs=periodic-kubevirt-plugins-e2e-previous-release)](https://testgrid.k8s.io/kubevirt-plugins-periodics#periodic-kubevirt-plugins-e2e-previous-release) | [![proto drift](https://prow.ci.kubevirt.io/badge.svg?jobs=periodic-kubevirt-plugins-hook-proto-drift)](https://testgrid.k8s.io/kubevirt-plugins-periodics#periodic-kubevirt-plugins-hook-proto-drift) |

Go SDK for writing [KubeVirt](https://kubevirt.io) plugins: hooks that customize virtual machines at defined points of their lifecycle without patching KubeVirt itself ([VEP 190](https://github.com/kubevirt/enhancements/tree/main/veps/sig-compute/190-kubevirt-structured-plugins)).
A plugin is a small Go program; the SDK handles the gRPC plumbing, and the plugin's `generate` command writes its image build and every deployment manifest.

> **Alpha** - the plugin API (KubeVirt v1.9+) is alpha. Expect breaking changes.

## Compatibility

SDK `vX.Y` targets KubeVirt `vX.Y`.
Pin the minor matching your cluster (`go get github.com/kubevirt/plugins@v1.10`) and update within it with `go get -u=patch`, since a new minor can break plugins.

## Quick start

Requires a cluster running KubeVirt v1.10 with the `Plugins` feature gate: add `Plugins` to `spec.configuration.developerConfiguration.featureGates` in the KubeVirt CR (`kubectl edit kubevirt -n kubevirt kubevirt`).

This plugin sets the SMBIOS system manufacturer of VMIs labelled `smbios-manufacturer=true` to `KubeVirt-<vmi name>`:

```go
package main

import (
	"context"

	"github.com/kubevirt/plugins/pkg/sdk/plugin"
)

type manufacturerHook struct{}

// GuestDefinition can run several times per VM start and sees its own earlier
// output, so the handler overwrites the entry KubeVirt set instead of adding one.
func (*manufacturerHook) GuestDefinition(_ context.Context, req *plugin.GuestDefinitionRequest) error {
	for _, info := range req.Domain.SysInfo {
		if info.SMBIOS == nil || info.SMBIOS.System == nil {
			continue
		}
		for i, entry := range info.SMBIOS.System.Entry {
			if entry.Name == "manufacturer" {
				info.SMBIOS.System.Entry[i].Value = "KubeVirt-" + req.VMI.Name
			}
		}
	}
	return nil
}

func main() {
	plugin.New("smbios-manufacturer").
		WithImage("quay.io/myorg/smbios-manufacturer:v0.1.0").
		WithLauncherHook(plugin.GuestDefinition(&manufacturerHook{}).WithCondition(`"smbios-manufacturer" in vmi.Labels && vmi.Labels["smbios-manufacturer"] == "true"`)).
		WithSidecarPodLabels(map[string]string{"smbios-manufacturer": "true"}).
		Execute() // "serve" runs the hook server, "generate" writes the build and deploy files
}
```

Build and deploy it, with `quay.io/myorg` replaced by a registry you can push to:

```bash
go mod init example.com/smbios-manufacturer
go get github.com/kubevirt/plugins@v1.10
go run . generate
make push
kubectl apply -f deploy/
```

`generate` writes everything needed to build and deploy the plugin, so there are no manifests to write by hand:

| File | Contents | Written when |
|---|---|---|
| `Containerfile` | multi-stage build into a `distroless/static` image that runs `serve` | the plugin serves hooks (not CEL-only) |
| `Makefile` | `build`, `push`, `test` and `vet` for the image set with `WithImage` | the plugin serves hooks |
| `deploy/NN-plugin.yaml` | the Plugin CR that registers the hooks with KubeVirt | always |
| `deploy/NN-mutating-admission-policy.yaml` and `-binding.yaml` | the admission policy that injects the plugin as a sidecar into matching virt-launcher pods | launcher hooks |
| `deploy/NN-daemonset.yaml` and `NN-rbac.yaml` | the DaemonSet serving node hooks on every node, and its RBAC from `WithRBACRules` | node hooks |

Files are numbered in apply order; this example gets `01-plugin.yaml`, `02-mutating-admission-policy.yaml` and `03-mutating-admission-policy-binding.yaml`.
Start a VMI labelled `smbios-manufacturer=true`; inside the guest, `cat /sys/class/dmi/id/sys_vendor` prints `KubeVirt-<vmi name>`.

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

Launcher hooks run in a sidecar container injected into the virt-launcher pod, next to the VM; node hooks run in a DaemonSet that virt-handler calls.
A plugin can mix both.
GuestDefinition changes that need no Go code can be a CEL expression instead (`plugin.GuestDefinitionCEL`), which KubeVirt evaluates itself without deploying a container.
Hooks can be skipped per VMI with CEL conditions, set to fail or ignore errors, and bounded by timeouts.
The godoc of [plugin](https://pkg.go.dev/github.com/kubevirt/plugins/pkg/sdk/plugin), [artifact](https://pkg.go.dev/github.com/kubevirt/plugins/pkg/sdk/artifact) (PreBoot) and [migration](https://pkg.go.dev/github.com/kubevirt/plugins/pkg/sdk/migration) (PreMigrationSource) covers these and the rules for writing each hook.

## Scoping sidecar injection

While the `Plugins` feature gate is on, the generated admission policy injects the sidecar into every virt-launcher pod, whether or not a hook condition matches the VMI.
A sidecar without resource requests makes the pod Burstable, which breaks dedicated-CPU VMIs, and an image that cannot be pulled keeps every VM from starting.
Scope it with `WithSidecarPodLabels` as the quick start does (KubeVirt copies VMI labels to the launcher pod), keep a hook condition on the same label so unlabelled VMIs do not wait for the sidecar, and set requests with `WithSidecarResources`.

## Examples

The plugins under [tests/](tests) are working examples and double as the SDK's functional tests:

- [guest_definition](tests/guest_definition/plugin.go) - a GuestDefinition hook, including a label-gated variant
- [pre_boot_artifact](tests/pre_boot_artifact/plugin.go) - rewriting the cloud-init ISO atomically
- [pre_migration_source](tests/pre_migration_source/plugin.go) - adjusting migration flags and parameters
- [node_hook_lifecycle](tests/node_hook_lifecycle/plugin.go) - node hooks across the VM lifecycle
- [multi_entrypoint](tests/multi_entrypoint/plugin.go) - splitting hooks across several containers

## Development

```bash
make test          # unit tests (pkg/...)
make vet
make cluster-up    # local kubevirtci cluster
make cluster-sync  # deploy KubeVirt (with the Plugins feature gate) and build the test plugin images
make functest      # functional tests in tests/ against the cluster
```

The test and vet targets run through a containerized Go toolchain, so no local Go is required.
Releases follow [RELEASING.md](RELEASING.md).
