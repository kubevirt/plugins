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

# "go run" uses the ginkgo version pinned in go.mod, avoiding a separate install step.
go run github.com/onsi/ginkgo/v2/ginkgo "${ARGS[@]}" \
    ./tests/ \
    -- \
    -kubeconfig="${KUBECONFIG}" \
    --artifacts="${ARTIFACTS}"
