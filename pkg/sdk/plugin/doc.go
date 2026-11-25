// Package plugin builds KubeVirt plugins: create one with [New], register
// hooks with the With* methods and call [Plugin.Execute] from main. The
// "serve" subcommand runs the hook servers and "generate" writes the
// Dockerfile, Makefile and deployment manifests.
//
// # Launcher and node hooks
//
// Launcher hooks run in a sidecar container that the generated admission
// policy injects into the virt-launcher pod; KubeVirt calls them over a unix
// socket. Node hooks run in a generated DaemonSet that virt-handler dials. A
// failing node hook fails the virt-handler sync, which is requeued and fires
// the hooks again, and virt-handler calls hooks from several workers at
// once, so node hook handlers must be idempotent and safe for concurrent use.
//
// # Conditions, failure strategy and timeouts
//
// A plugin, and each of its hooks, can carry a CEL condition evaluated
// against the VMI (WithCondition); a false condition skips the hook. The only
// variable is vmi, addressed with Go struct field names
// (vmi.Spec.Domain.CPU.Cores). Evaluation errors are hard failures regardless
// of the failure strategy, so guard map access:
//
//	"k" in vmi.Labels && vmi.Labels["k"] == "v"
//
// Conditions are validated when the plugin is constructed, and
// cel.EvaluateCondition evaluates one locally in unit tests. A failing hook
// fails the VM start, migration or sync unless WithFailureStrategy sets it
// to [Ignore]; WithTimeout bounds each call.
//
// # CEL GuestDefinition hooks
//
// A [GuestDefinitionCEL] expression sees vmi and domainSpec and returns a
// Domain{...} struct literal. KubeVirt evaluates it inside virt-launcher, so
// no container is deployed and generate emits only the Plugin CR. The result
// is deep-merged: only fields present in the literal are written and lists
// are replaced wholesale, so rebuild a list from domainSpec to keep its
// entries.
//
// # Domain round trip
//
// When a GuestDefinition hook, CEL or sidecar, is selected for a VMI,
// virt-launcher converts the domain to libvirtxml and back. Elements and
// attributes that the libvirtxml version vendored by KubeVirt does not model
// are silently dropped, so a hook cannot add them.
//
// # Entrypoints
//
// WithEntrypoint splits a plugin's hooks across several sidecar containers
// or DaemonSets: generate emits one admission policy and DaemonSet per
// entrypoint, and each container serves only its subset. A hook point can be
// registered once per entrypoint, so compose transformations in one handler.
// Plugin images must keep ENTRYPOINT ["<binary>", "serve"] like the
// generated Dockerfile, since the manifests pass their flags as container
// args.
package plugin
