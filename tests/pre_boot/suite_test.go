package main

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubevirt/plugins/tests/framework"
)

func TestPreBoot(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Pre Boot Suite")
}

var _ = framework.SetupSuite()
