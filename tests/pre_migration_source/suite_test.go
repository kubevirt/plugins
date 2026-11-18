package main

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubevirt/plugins/tests/framework"
)

func TestPreMigrationSource(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Pre Migration Source Suite")
}

var _ = framework.SetupSuite()
