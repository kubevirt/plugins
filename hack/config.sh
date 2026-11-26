#!/usr/bin/env bash
# Central configuration for the test infrastructure.
# All variables can be overridden via environment.

# KubeVirt to deploy into the test cluster (resolved by hack/deploy-kubevirt.sh):
#   vX.Y.Z[-pre]  an exact release tag, e.g. v1.9.1 or v1.10.0-rc.0
#   vX.Y          the latest vX.Y.Z release, pre-releases excluded
#   nightly       the latest nightly build of KubeVirt main
export KUBEVIRT_VERSION=${KUBEVIRT_VERSION:-"v1.10.0"}
# KubeVirt ref the hook protocol in api/hooks/v1alpha1 is synced from;
# pinned to the KubeVirt tag matching kubevirt.io/api in go.mod, so the two
# move together. A periodic job can override it to catch upstream drift.
export KUBEVIRT_PROTO_REF=${KUBEVIRT_PROTO_REF:-"v1.10.0"}
# Number of kubevirtci worker nodes. Live migration functests need two nodes.
export KUBEVIRT_NUM_NODES=${KUBEVIRT_NUM_NODES:-2}
# Tag applied to test plugin container images built by cluster-build.sh.
export DOCKER_TAG=${DOCKER_TAG:-"latest"}
