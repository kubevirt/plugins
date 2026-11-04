#!/usr/bin/env bash
# Periodic e2e entry point for stable SDK releases: clones one of the newest
# release-X.Y branches and runs its functional tests against KubeVirt vX.Y.
set -euo pipefail

# Capture an explicit pin before anything below can touch KUBEVIRT_VERSION.
EXPLICIT_KUBEVIRT_VERSION=${KUBEVIRT_VERSION:-}

: "${RELEASE_OFFSET:?RELEASE_OFFSET must be set: 0 = newest SDK release branch, 1 = the one before}"

PLUGINS_REPO=${PLUGINS_REPO:-"https://github.com/kubevirt/plugins.git"}
KUBEVIRT_REPO="https://github.com/kubevirt/kubevirt"

if ! [[ "${RELEASE_OFFSET}" =~ ^[0-9]+$ ]]; then
    echo "RELEASE_OFFSET must be a non-negative integer, got '${RELEASE_OFFSET}'" >&2
    exit 1
fi

# Discover the release branches. ls-remote needs no local repository; -C /
# keeps it independent of this checkout's .git (see hack/deploy-kubevirt.sh).
refs=$(git -C / ls-remote --heads "${PLUGINS_REPO}" 'refs/heads/release-*')
mapfile -t branches < <(grep -oE 'release-[0-9]+\.[0-9]+$' <<<"${refs}" | sort -rV)
if [ "${#branches[@]}" -le "${RELEASE_OFFSET}" ]; then
    echo "Only ${#branches[@]} SDK release branch(es) in ${PLUGINS_REPO}; nothing at RELEASE_OFFSET=${RELEASE_OFFSET}."
    exit 0
fi
branch=${branches[${RELEASE_OFFSET}]}

# KubeVirt vX.Y once a GA vX.Y.Z tag exists (hack/deploy-kubevirt.sh resolves
# it to the newest patch release); until then only rcs are out, so leave the
# variable unset and let the branch's own hack/config.sh default (an rc) apply.
minor="v${branch#release-}"
if [ -n "${EXPLICIT_KUBEVIRT_VERSION}" ]; then
    export KUBEVIRT_VERSION="${EXPLICIT_KUBEVIRT_VERSION}"
    version_desc="KUBEVIRT_VERSION=${KUBEVIRT_VERSION} (pinned by caller)"
else
    tags=$(git -C / ls-remote --tags --refs "${KUBEVIRT_REPO}" "refs/tags/${minor}.*")
    if grep -qE "refs/tags/${minor//./\\.}\.[0-9]+$" <<<"${tags}"; then
        export KUBEVIRT_VERSION="${minor}"
        version_desc="KUBEVIRT_VERSION=${KUBEVIRT_VERSION} (KubeVirt ${minor} GA is out)"
    else
        unset KUBEVIRT_VERSION
        version_desc="KUBEVIRT_VERSION unset; KubeVirt ${minor} GA not out yet, the branch default applies"
    fi
fi

echo "Testing ${branch} of ${PLUGINS_REPO}"
echo "${version_desc}"

if [ "${DRY_RUN:-}" = "true" ]; then
    echo "DRY_RUN=true, stopping before the clone."
    exit 0
fi

workdir=$(mktemp -d)

function cleanup() {
    # Best effort: cluster-down must not mask the make result, and the temp
    # dir is removed whether or not it succeeded.
    (cd "${workdir}" && make cluster-down) || true
    rm -rf "${workdir}"
}
trap cleanup EXIT

# A plain clone, not a worktree: the caller's checkout and its .git (which may
# be unusable inside containers) must not be touched.
git clone --depth 1 --branch "${branch}" "${PLUGINS_REPO}" "${workdir}"

# cd rather than make -C: the cluster scripts locate kubevirtci via ${PWD}.
cd "${workdir}"
make cluster-up cluster-sync functest
