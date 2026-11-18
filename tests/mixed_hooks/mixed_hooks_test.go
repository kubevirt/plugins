package main

import (
	. "github.com/onsi/ginkgo/v2"

	"github.com/kubevirt/plugins/tests/framework"
)

var _ = Describe("Mixed Hooks Plugin", func() {
	It("should start a VMI with domain and node hooks on the default entrypoint", func() {
		ns := framework.TestContext.Namespace

		p := newPlugin().WithNamespace(ns).WithImage(framework.TestContext.ContainerPrefix + "/mixed_hooks:latest")

		framework.TestContext.DeployPlugin(p, ns)

		framework.TestContext.WaitForDaemonSetReady(ns, "test-mixed-hooks")

		// The sidecar must serve only the launcher hook: it runs as the qemu
		// user and cannot create the node hook socket.
		vmi := framework.TestContext.CreateVMI(ns)
		framework.TestContext.WaitForVMIRunning(ns, vmi.Name)
	})
})
