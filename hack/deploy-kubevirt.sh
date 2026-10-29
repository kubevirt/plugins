#!/usr/bin/env bash
# Downloads and deploys the KubeVirt manifests selected by KUBEVIRT_VERSION
# (see hack/config.sh), then patches the KubeVirt CR to enable the Plugins
# feature gate.
set -e

source hack/config.sh

KUBECTL="cluster/kubectl.sh"
OUT_DIR="_out/manifests"
KUBEVIRT_REPO="https://github.com/kubevirt/kubevirt"
RELEASE_URL="${KUBEVIRT_REPO}/releases/download"
NIGHTLY_URL="https://storage.googleapis.com/kubevirt-prow/devel/nightly/release/kubevirt/kubevirt"

# Prints the latest release of the given vX.Y minor. Pre-releases are skipped,
# and so are tags whose release manifests are not published yet.
function latest_release() {
    local minor=$1 refs tag code
    # ls-remote needs no local repository. Run it from / so it does not depend
    # on the state of this checkout's .git (e.g. a worktree whose gitdir is
    # not mounted into the container running this script).
    if ! refs=$(git -C / ls-remote --tags --refs "${KUBEVIRT_REPO}" "refs/tags/${minor}.*"); then
        echo "Failed to list KubeVirt tags" >&2
        return 1
    fi
    for tag in $(echo "${refs}" | sed 's|.*refs/tags/||' | grep -E "^${minor//./\\.}\.[0-9]+$" | sort -rV); do
        # Only a 404 moves on to the previous tag. Any other failure must not
        # silently resolve to an older release.
        code=$(curl -sIL --retry 3 --max-time 60 -o /dev/null -w '%{http_code}' \
            "${RELEASE_URL}/${tag}/kubevirt-operator.yaml") || code="curl exit code $?"
        case "${code}" in
        200)
            echo "${tag}"
            return 0
            ;;
        404) ;;
        *)
            echo "Failed to check the KubeVirt ${tag} release manifests: ${code}" >&2
            return 1
            ;;
        esac
    done
    echo "No ${minor}.Z release of KubeVirt found" >&2
    return 1
}

# --- Resolve KUBEVIRT_VERSION (see hack/config.sh) ---

if [[ "${KUBEVIRT_VERSION}" == "nightly" ]]; then
    build=$(curl -fsSL --retry 3 "${NIGHTLY_URL}/latest")
    if [[ ! "${build}" =~ ^[0-9]+$ ]]; then
        echo "Unexpected KubeVirt nightly build ID: '${build}'" >&2
        exit 1
    fi
    resolved="nightly ${build}"
    BASE_URL="${NIGHTLY_URL}/${build}"
elif [[ "${KUBEVIRT_VERSION}" =~ ^v[0-9]+\.[0-9]+$ ]]; then
    resolved=$(latest_release "${KUBEVIRT_VERSION}")
    BASE_URL="${RELEASE_URL}/${resolved}"
else
    resolved="${KUBEVIRT_VERSION}"
    BASE_URL="${RELEASE_URL}/${resolved}"
fi
echo "Resolved KUBEVIRT_VERSION=${KUBEVIRT_VERSION} to ${resolved}"

# --- Download manifests ---

mkdir -p "${OUT_DIR}"
echo "Downloading KubeVirt ${resolved} manifests..."
curl -fL --retry 3 "${BASE_URL}/kubevirt-operator.yaml" -o "${OUT_DIR}/kubevirt-operator.yaml"
curl -fL --retry 3 "${BASE_URL}/kubevirt-cr.yaml" -o "${OUT_DIR}/kubevirt-cr.yaml"

source cluster/kubevirtci.sh
kubevirtci::install

# --- Deploy operator ---

echo "Deploying KubeVirt operator..."
# Server-side apply: the CRDs of recent KubeVirt builds exceed the
# annotation size limit of client-side apply.
${KUBECTL} apply --server-side --force-conflicts -f "${OUT_DIR}/kubevirt-operator.yaml"
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
echo "KubeVirt ${resolved} is ready with Plugins feature gate enabled."
