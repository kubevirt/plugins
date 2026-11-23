package main

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubevirt/plugins/tests/framework"
)

func TestPreBootArtifact(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "PreBoot Artifact Suite")
}

var _ = framework.SetupSuite()
