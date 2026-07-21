package main_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/iholder101/kubevirt-plugins/tests/framework"
)

func TestMultiEntrypoint(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Multi-Entrypoint Suite")
}

var _ = framework.SetupSuite()
