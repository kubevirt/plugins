#!/usr/bin/env bash
# Central configuration for the test infrastructure.
# All variables can be overridden via environment.

# KubeVirt to deploy into the test cluster (resolved by hack/deploy-kubevirt.sh):
#   vX.Y.Z[-pre]  an exact release tag, e.g. v1.9.1 or v1.10.0-rc.0
#   vX.Y          the latest vX.Y.Z release, pre-releases excluded
#   nightly       the latest nightly build of KubeVirt main
export KUBEVIRT_VERSION=${KUBEVIRT_VERSION:-"v1.9.1"}
# Number of kubevirtci worker nodes. 1 is enough for plugin tests.
export KUBEVIRT_NUM_NODES=${KUBEVIRT_NUM_NODES:-1}
# Tag applied to test plugin container images built by cluster-build.sh.
export DOCKER_TAG=${DOCKER_TAG:-"latest"}
