package main_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubevirt/plugins/tests/framework"
)

func TestNodeHookLifecycle(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Node Hook Lifecycle Suite")
}

var _ = framework.SetupSuite()
