package main_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubevirt/plugins/tests/framework"
)

func TestMixedHooks(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Mixed Hooks Suite")
}

var _ = framework.SetupSuite()
