package main

import (
	"testing"

	"github.com/kubevirt/plugins/tests/framework"
)

func TestMultiEntrypoint(t *testing.T) {
	framework.RunSuite(t, "Multi-Entrypoint Suite")
}

var _ = framework.SetupSuite()
