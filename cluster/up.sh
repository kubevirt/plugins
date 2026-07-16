#!/usr/bin/env bash
# Brings up a kubevirtci cluster with the provider and node count from config.
set -ex

source hack/config.sh
source cluster/kubevirtci.sh
kubevirtci::install

"$(kubevirtci::path)/cluster-up/up.sh"
