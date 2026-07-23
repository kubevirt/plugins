package main_test

import (
	. "github.com/onsi/ginkgo/v2"

	"github.com/iholder101/kubevirt-plugins/pkg/sdk/plugin"
	"github.com/iholder101/kubevirt-plugins/tests/framework"
)

var _ = Describe("Node Hook Lifecycle", func() {
	It("should deploy node hook DaemonSet and start VMI successfully", func() {
		ns := framework.TestContext.Namespace

		p := plugin.New("test-node-hook").
			WithNodeHook(plugin.PreVMStart, plugin.NodeHandler(&framework.NoopNodeHandler{})).
			WithNamespace(ns).
			WithImage(framework.TestContext.ContainerPrefix + "/node_hook_lifecycle:latest")

		framework.TestContext.DeployPlugin(p, ns)

		framework.TestContext.WaitForDaemonSetReady(ns, "test-node-hook")

		vmi := framework.TestContext.CreateVMI(ns)
		framework.TestContext.WaitForVMIRunning(ns, vmi.Name)
	})
})
