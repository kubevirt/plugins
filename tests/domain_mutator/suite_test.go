package main_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/iholder101/kubevirt-plugins/tests/framework"
)

func TestDomainMutator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Domain Mutator Suite")
}

var _ = framework.SetupSuite()
