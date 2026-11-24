package main

import (
	"testing"

	"github.com/kubevirt/plugins/tests/framework"
)

func TestPreBootArtifact(t *testing.T) {
	framework.RunSuite(t, "PreBoot Artifact Suite")
}

var _ = framework.SetupSuite()
