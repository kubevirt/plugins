#!/usr/bin/env bash
# Downloads and deploys KubeVirt from release manifests, then patches the
# KubeVirt CR to enable the Plugins feature gate.
set -e

source hack/config.sh
source cluster/kubevirtci.sh
kubevirtci::install

KUBECTL="cluster/kubectl.sh"
OUT_DIR="_out/manifests"
mkdir -p "${OUT_DIR}"

# --- Download release manifests ---

BASE_URL="https://github.com/kubevirt/kubevirt/releases/download/${KUBEVIRT_VERSION}"

echo "Downloading KubeVirt ${KUBEVIRT_VERSION} manifests..."
curl -fL "${BASE_URL}/kubevirt-operator.yaml" -o "${OUT_DIR}/kubevirt-operator.yaml"
curl -fL "${BASE_URL}/kubevirt-cr.yaml" -o "${OUT_DIR}/kubevirt-cr.yaml"

# --- Deploy operator ---

echo "Deploying KubeVirt operator..."
${KUBECTL} apply -f "${OUT_DIR}/kubevirt-operator.yaml"
${KUBECTL} -n kubevirt wait --for=condition=Available deployment/virt-operator --timeout=300s
echo "KubeVirt operator is ready."

# --- Apply CR and enable Plugins feature gate ---
# We apply the stock CR first, then patch it with kubectl rather than
# pre-patching the YAML with yq, to avoid an extra tool dependency.

echo "Applying KubeVirt CR with Plugins feature gate..."
${KUBECTL} apply -f "${OUT_DIR}/kubevirt-cr.yaml"
${KUBECTL} patch kv kubevirt -n kubevirt --type=merge \
    -p '{"spec":{"configuration":{"developerConfiguration":{"featureGates":["Plugins"]}}}}'

echo "Waiting for KubeVirt to become available..."
${KUBECTL} -n kubevirt wait kv/kubevirt --for=condition=Available --timeout=600s
echo "KubeVirt ${KUBEVIRT_VERSION} is ready with Plugins feature gate enabled."
