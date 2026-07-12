#!/usr/bin/env bash
# Copies api.proto and api.pb.go from the kubevirt repo and updates
# the Go package paths to point to this module.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

SRC_DIR="../kubevirt/pkg/hooks/plugins/v1alpha1"
DST_DIR="$REPO_ROOT/api/hooks/v1alpha1"

OLD_PKG="kubevirt.io/kubevirt/pkg/hooks/plugins/v1alpha1"
NEW_PKG="github.com/iholder101/kubevirt-plugins/api/hooks/v1alpha1"

if [[ ! -d "$SRC_DIR" ]]; then
    echo "Error: kubevirt source not found at $SRC_DIR" >&2
    echo "Expected ../kubevirt/ to be a sibling directory." >&2
    exit 1
fi

mkdir -p "$DST_DIR"

# Copy api.proto
cp "$SRC_DIR/api.proto" "$DST_DIR/api.proto"

# Update or add go_package option in api.proto
if grep -q 'option go_package' "$DST_DIR/api.proto"; then
    sed -i "s|option go_package = \"$OLD_PKG\"|option go_package = \"$NEW_PKG\"|" "$DST_DIR/api.proto"
else
    # Add go_package after the package declaration
    sed -i "/^package kubevirt.hooks.plugins.v1alpha1;/a\\\\noption go_package = \"$NEW_PKG\";" "$DST_DIR/api.proto"
fi

# Copy api.pb.go
cp "$SRC_DIR/api.pb.go" "$DST_DIR/api.pb.go"

# Replace any kubevirt import paths in api.pb.go
sed -i "s|$OLD_PKG|$NEW_PKG|g" "$DST_DIR/api.pb.go"

echo "Updated api.proto and api.pb.go in $DST_DIR"
