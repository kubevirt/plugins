package main

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubevirt/plugins/tests/framework"
)

func TestGuestDefinition(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Guest Definition Suite")
}

var _ = framework.SetupSuite()
