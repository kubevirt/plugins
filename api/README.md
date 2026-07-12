# API Types

The proto definitions and generated Go code under `hooks/v1alpha1/` are
vendored from [kubevirt/kubevirt](https://github.com/kubevirt/kubevirt)
(`pkg/hooks/plugins/v1alpha1/`).

These types will eventually move to a standalone, versioned module so that
both kubevirt and plugin authors can import them without pulling in the
full kubevirt dependency tree.

Until then, keep these files in sync with upstream manually.
