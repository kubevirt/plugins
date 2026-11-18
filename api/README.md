# API Types

The proto definitions and generated Go code under `hooks/v1alpha1/` are
vendored from [kubevirt/kubevirt](https://github.com/kubevirt/kubevirt)
(`pkg/hooks/plugins/v1alpha1/`).

These types will eventually move to a standalone, versioned module so that
both kubevirt and plugin authors can import them without pulling in the
full kubevirt dependency tree.

Until then, keep these files in sync with upstream by running
`make update-proto`. The files are fetched from the kubevirt ref in
`KUBEVIRT_PROTO_REF` (a commit pinned in `hack/config.sh`, moved together
with the `kubevirt.io/api` dependency): via `curl` from
raw.githubusercontent.com, or via `git show` from a local clone when
`KUBEVIRT_DIR` points at one (the ref is resolved, nothing is checked
out). The proto is given a `go_package` option pointing at this module
and the Go files get their import paths rewritten accordingly.
`make verify-proto` checks the vendored files against that ref and fails
when they drift.
