package main

import (
	"testing"

	"github.com/kubevirt/plugins/tests/framework"
)

func TestPreMigrationSource(t *testing.T) {
	framework.RunSuite(t, "Pre Migration Source Suite")
}

var _ = framework.SetupSuite()
