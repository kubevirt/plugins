package main

import (
	"testing"

	"github.com/kubevirt/plugins/tests/framework"
)

func TestGuestDefinition(t *testing.T) {
	framework.RunSuite(t, "Guest Definition Suite")
}

var _ = framework.SetupSuite()
