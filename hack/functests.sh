#!/usr/bin/env bash
# Builds and runs the Ginkgo functional test suite against the kubevirtci cluster.
set -e

source hack/config.sh
source cluster/kubevirtci.sh
kubevirtci::install

KUBECONFIG=$(kubevirtci::kubeconfig)
export KUBECONFIG
ARTIFACTS=${ARTIFACTS:-"_out/artifacts"}
mkdir -p "${ARTIFACTS}"

KUBEVIRTCI_CONFIG_PATH="$(kubevirtci::path)/_ci-configs"
source "${KUBEVIRTCI_CONFIG_PATH}/${KUBEVIRT_PROVIDER}/config-provider-${KUBEVIRT_PROVIDER}.sh"
: ${manifest_docker_prefix:?"manifest_docker_prefix not set - is the cluster running?"}

if [ ! -d "tests" ]; then
    echo "No tests/ directory found. Skipping functional tests."
    exit 0
fi

ARGS=("--timeout=1h" "-v")

# Allow callers to inject extra ginkgo flags (e.g., --focus, --label-filter).
if [ -n "${FUNC_TEST_ARGS}" ]; then
    read -ra extra <<< "${FUNC_TEST_ARGS}"
    ARGS+=("${extra[@]}")
fi

# "go run" uses the ginkgo version pinned in go.mod, avoiding a separate
# install step. Runs inside the dockerized builder by default so no local Go
# toolchain is required; needs host networking to reach the kubevirtci
# cluster's API server. Set KUBEVIRT_PLUGINS_SKIP_DOCKERIZED=true to run directly.
#
# Ginkgo changes its working directory to each suite's package directory
# before running it, so -kubeconfig must be an absolute path - hence the two
# different values below rather than one path shared across both branches.
# hack/dockerized always bind-mounts the repo root at /workspace.
#
# The suites call the kubevirtci kubectl binary directly instead of
# cluster/kubectl.sh, because kubevirtci's wrapper refuses to run without a
# container runtime, which the builder container does not have. The paths
# differ between the two branches for the same reason as -kubeconfig.
if [ "${KUBEVIRT_PLUGINS_SKIP_DOCKERIZED:-}" = "true" ]; then
    go run github.com/onsi/ginkgo/v2/ginkgo "${ARGS[@]}" ./tests/... -- -kubeconfig="${KUBECONFIG}" --kubectl="${kubectl}" --container-prefix="${manifest_docker_prefix}" --artifacts="${ARTIFACTS}"
else
    DOCKERIZED_EXTRA_ARGS="--network=host" hack/dockerized go run github.com/onsi/ginkgo/v2/ginkgo "${ARGS[@]}" ./tests/... -- -kubeconfig="/workspace/_kubevirtci/_ci-configs/${KUBEVIRT_PROVIDER}/.kubeconfig" --kubectl="/workspace/_kubevirtci/_ci-configs/${KUBEVIRT_PROVIDER}/.kubectl" --container-prefix="${manifest_docker_prefix}" --artifacts="${ARTIFACTS}"
fi
