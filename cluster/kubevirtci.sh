# Manages the kubevirtci clone-on-demand lifecycle. Clones kubevirtci at the
# tag in cluster/kubevirtci_tag.txt into _kubevirtci/. Sourced by cluster/*.sh.

export KUBEVIRT_PROVIDER=${KUBEVIRT_PROVIDER:-'k8s-1.36'}
export KUBEVIRTCI_TAG=${KUBEVIRTCI_TAG:-$(cat cluster/kubevirtci_tag.txt)}

KUBEVIRTCI_PATH="${PWD}/_kubevirtci"
KUBEVIRTCI_REPO='https://github.com/kubevirt/kubevirtci.git'

function cluster::_get_repo() {
    git --git-dir "${KUBEVIRTCI_PATH}/.git" remote get-url origin
}

function cluster::_get_tag() {
    git -C "${KUBEVIRTCI_PATH}" describe --tags
}

function kubevirtci::install() {
    # If an existing clone points to the wrong repo or tag, wipe it so we
    # re-clone the correct one below.
    if [ -d "${KUBEVIRTCI_PATH}" ]; then
        if [ "$(cluster::_get_repo)" != "${KUBEVIRTCI_REPO}" ] || [ "$(cluster::_get_tag)" != "${KUBEVIRTCI_TAG}" ]; then
            rm -rf "${KUBEVIRTCI_PATH}"
        fi
    fi

    if [ ! -d "${KUBEVIRTCI_PATH}" ]; then
        git clone "${KUBEVIRTCI_REPO}" "${KUBEVIRTCI_PATH}"
        (
            cd "${KUBEVIRTCI_PATH}" || exit
            git checkout "tags/${KUBEVIRTCI_TAG}" -b "${KUBEVIRTCI_TAG}"
        )
    fi
}

function kubevirtci::path() {
    echo -n "${KUBEVIRTCI_PATH}"
}

function kubevirtci::kubeconfig() {
    echo -n "${KUBEVIRTCI_PATH}/_ci-configs/${KUBEVIRT_PROVIDER}/.kubeconfig"
}
