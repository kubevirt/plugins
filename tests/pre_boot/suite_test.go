package main

import (
	"testing"

	"github.com/kubevirt/plugins/tests/framework"
)

func TestPreBoot(t *testing.T) {
	framework.RunSuite(t, "Pre Boot Suite")
}

var _ = framework.SetupSuite()
