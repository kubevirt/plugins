# The test and vet targets auto-wrap through hack/dockerized so no local Go
# toolchain is required. Set KUBEVIRT_PLUGINS_SKIP_DOCKERIZED=true to skip the
# wrapper and run Go directly (e.g. when Go is available locally, or when
# already running inside the builder container). The functest target
# dockerizes the same way internally (see hack/functests.sh): only its final
# ginkgo invocation needs Go, so that's the only part that runs containerized.
#
# Tests are scoped to ./pkg/... on purpose: the suites under tests/ are
# Ginkgo functional tests that need a live cluster and are run via the
# functest target, not plain `go test`. Vet, in contrast, covers ./... so
# the functional test packages are compile-checked on every run.
#
# cluster-up, cluster-down, and cluster-sync run directly on the host: they
# must talk to the host's own podman/docker to create cluster-node
# containers that stay reachable (via kubectl, functest, cluster-down) after
# the script exits, so dockerizing them would only relocate the same
# host-engine dependency, not remove it.
ifneq ($(KUBEVIRT_PLUGINS_SKIP_DOCKERIZED),true)
test:
	hack/dockerized make $@
vet:
	hack/dockerized make $@
else
test:
	go test ./pkg/...
vet:
	go vet ./...
endif

# update-proto and verify-proto run on the host like the cluster targets:
# they only need bash, curl or git, and diff, not a Go toolchain.
update-proto:
	./hack/update-proto.sh

verify-proto:
	./hack/verify-proto.sh

.PHONY: test vet update-proto verify-proto cluster-up cluster-down cluster-sync deploy-kubevirt build-test-plugins functest

cluster-up:
	./cluster/up.sh

cluster-down:
	./cluster/down.sh

cluster-sync: deploy-kubevirt build-test-plugins

deploy-kubevirt:
	./hack/deploy-kubevirt.sh

build-test-plugins:
	./hack/cluster-build.sh

functest:
	./hack/functests.sh
