#!/usr/bin/env bash
# Fetches the hook protocol files from kubevirt at KUBEVIRT_PROTO_REF and
# writes them, rewritten for this module, into api/hooks/v1alpha1.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

source "$SCRIPT_DIR/config.sh"
source "$SCRIPT_DIR/proto-common.sh"

DST_DIR="$REPO_ROOT/api/hooks/v1alpha1"

write_proto_files "$DST_DIR"

echo "Updated ${PROTO_FILES[*]} in $DST_DIR from kubevirt $KUBEVIRT_PROTO_REF"
