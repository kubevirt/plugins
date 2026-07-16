.PHONY: test cluster-up cluster-down cluster-sync deploy-kubevirt build-test-plugins functest

test:
	go test ./...

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
