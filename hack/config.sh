#!/usr/bin/env bash
# Central configuration for the test infrastructure.
# All variables can be overridden via environment.

# KubeVirt release to deploy into the test cluster.
export KUBEVIRT_VERSION=${KUBEVIRT_VERSION:-"v1.9.0-rc.0"}
# Number of kubevirtci worker nodes. 1 is enough for plugin tests.
export KUBEVIRT_NUM_NODES=${KUBEVIRT_NUM_NODES:-1}
# Tag applied to test plugin container images built by cluster-build.sh.
export DOCKER_TAG=${DOCKER_TAG:-"latest"}
