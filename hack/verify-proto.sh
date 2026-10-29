#!/usr/bin/env bash
# Verifies that api/hooks/v1alpha1 matches kubevirt at KUBEVIRT_PROTO_REF
# after the update-proto.sh rewrites.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

source "$SCRIPT_DIR/config.sh"
source "$SCRIPT_DIR/proto-common.sh"

DST_DIR="$REPO_ROOT/api/hooks/v1alpha1"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

write_proto_files "$TMP_DIR"

drift=0
for file in "${PROTO_FILES[@]}"; do
    if ! diff -u "$DST_DIR/$file" "$TMP_DIR/$file"; then
        drift=1
    fi
done

if [[ "$drift" -ne 0 ]]; then
    echo "Error: api/hooks/v1alpha1 differs from kubevirt $KUBEVIRT_PROTO_REF; run make update-proto" >&2
    exit 1
fi

echo "api/hooks/v1alpha1 is in sync with kubevirt $KUBEVIRT_PROTO_REF"
