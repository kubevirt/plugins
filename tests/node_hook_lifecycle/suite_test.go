package main

import (
	"testing"

	"github.com/kubevirt/plugins/tests/framework"
)

func TestNodeHookLifecycle(t *testing.T) {
	framework.RunSuite(t, "Node Hook Lifecycle Suite")
}

var _ = framework.SetupSuite()
