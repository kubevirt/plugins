package main

import (
	"testing"

	"github.com/kubevirt/plugins/tests/framework"
)

func TestMixedHooks(t *testing.T) {
	framework.RunSuite(t, "Mixed Hooks Suite")
}

var _ = framework.SetupSuite()
