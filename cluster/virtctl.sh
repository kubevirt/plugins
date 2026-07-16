#!/usr/bin/env bash
# Runs virtctl against the kubevirtci cluster. All arguments are forwarded.
set -e

source cluster/kubevirtci.sh
kubevirtci::install

"$(kubevirtci::path)/cluster-up/cli.sh" "$@"
