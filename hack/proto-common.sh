#!/usr/bin/env bash
# Shared helpers for fetching the kubevirt hook protocol files and
# rewriting them for this module. Sourced by hack/update-proto.sh and
# hack/verify-proto.sh, not run directly.

PROTO_SRC_DIR="pkg/hooks/plugins/v1alpha1"
PROTO_FILES=(api.proto api.pb.go v1alpha1.go)
KUBEVIRT_RAW_BASE_URL="https://raw.githubusercontent.com/kubevirt/kubevirt"

# Go package of the hook protocol in kubevirt, and of its copy in this module.
KUBEVIRT_GO_PKG="kubevirt.io/kubevirt/pkg/hooks/plugins/v1alpha1"
PLUGINS_GO_PKG="github.com/kubevirt/plugins/api/hooks/v1alpha1"

# resolve_proto_ref prints the commit KUBEVIRT_PROTO_REF resolves to in
# $KUBEVIRT_DIR, trying the plain ref before its upstream/ and origin/ spellings.
resolve_proto_ref() {
    local candidate commit
    for candidate in "$KUBEVIRT_PROTO_REF" "upstream/$KUBEVIRT_PROTO_REF" "origin/$KUBEVIRT_PROTO_REF"; do
        if commit=$(git -C "$KUBEVIRT_DIR" rev-parse --verify --quiet "${candidate}^{commit}"); then
            echo "$commit"
            return 0
        fi
    done
    echo "Error: ref \"$KUBEVIRT_PROTO_REF\" does not resolve to a commit in $KUBEVIRT_DIR" >&2
    return 1
}

# fetch_proto_file prints the raw upstream file $1 at ref $2, read from
# $KUBEVIRT_DIR when set and from GitHub otherwise.
fetch_proto_file() {
    local file="$1" ref="$2"
    if [[ -n "${KUBEVIRT_DIR:-}" ]]; then
        git -C "$KUBEVIRT_DIR" show "$ref:$PROTO_SRC_DIR/$file"
    else
        curl -fsSL "$KUBEVIRT_RAW_BASE_URL/$ref/$PROTO_SRC_DIR/$file"
    fi
}

# transform_proto_file rewrites $1 in place for this module: the proto gains a
# go_package option and the Go files get their import paths rewritten.
transform_proto_file() {
    local file="$1"
    case "$file" in
    *.proto)
        if grep -q '^option go_package' "$file"; then
            sed -i "s|option go_package = \"$KUBEVIRT_GO_PKG\"|option go_package = \"$PLUGINS_GO_PKG\"|" "$file"
        else
            sed -i "/^package kubevirt.hooks.plugins.v1alpha1;/a\\\\noption go_package = \"$PLUGINS_GO_PKG\";" "$file"
        fi
        if ! grep -q "^option go_package = \"$PLUGINS_GO_PKG\";$" "$file"; then
            echo "Error: $file is missing the go_package option for $PLUGINS_GO_PKG; the package declaration may have moved" >&2
            return 1
        fi
        ;;
    *.go)
        sed -i "s|$KUBEVIRT_GO_PKG|$PLUGINS_GO_PKG|g" "$file"
        ;;
    esac
}

# write_proto_files fetches the hook protocol files at KUBEVIRT_PROTO_REF,
# rewrites them for this module, and writes them into $1. Each file is
# fetched into a temporary copy first, so a failed fetch never leaves a
# truncated file behind.
write_proto_files() {
    local dst_dir="$1" file ref tmp
    if [[ -n "${KUBEVIRT_DIR:-}" ]]; then
        ref=$(resolve_proto_ref)
    else
        ref="$KUBEVIRT_PROTO_REF"
    fi

    mkdir -p "$dst_dir"
    for file in "${PROTO_FILES[@]}"; do
        tmp="$dst_dir/$file.tmp"
        if ! fetch_proto_file "$file" "$ref" > "$tmp"; then
            rm -f "$tmp"
            echo "Error: failed to fetch $PROTO_SRC_DIR/$file at ref \"$ref\"" >&2
            return 1
        fi
        mv "$tmp" "$dst_dir/$file"
        transform_proto_file "$dst_dir/$file"
    done
}
