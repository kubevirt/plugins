#!/usr/bin/env bash
# Tears down the kubevirtci cluster.
set -ex

source cluster/kubevirtci.sh
kubevirtci::install

"$(kubevirtci::path)/cluster-up/down.sh"
