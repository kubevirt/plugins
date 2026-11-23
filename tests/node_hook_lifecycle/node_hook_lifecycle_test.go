package main

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubevirt/plugins/tests/framework"
)

var _ = Describe("Node Hook Lifecycle", func() {
	const pluginName = "test-node-hook-lifecycle"

	It("should run node hooks across the VM lifecycle", func() {
		ns := framework.TestContext.Namespace

		framework.TestContext.DeploySuitePlugin(newPlugin(), ns, "node_hook_lifecycle")
		framework.TestContext.WaitForDaemonSetReady(ns, pluginName)

		vmi := framework.TestContext.CreateVMI(ns)
		framework.TestContext.WaitForVMIRunning(ns, vmi.Name)

		nodeName := framework.TestContext.GetVMI(ns, vmi.Name).Status.NodeName
		pod := framework.TestContext.DaemonSetPodOnNode(ns, pluginName, nodeName)
		logs := func() string {
			return framework.TestContext.ContainerLogs(ns, pod.Name, pluginName)
		}

		Expect(logs()).To(ContainSubstring(fmt.Sprintf("node hook PreVMStart vmi=%s/%s", ns, vmi.Name)))

		framework.TestContext.DeleteVMI(ns, vmi.Name)

		Eventually(logs, 30*time.Second, 2*time.Second).Should(ContainSubstring(fmt.Sprintf("node hook OnVMStop vmi=%s/%s", ns, vmi.Name)))
		Eventually(logs, 60*time.Second, 2*time.Second).Should(ContainSubstring(fmt.Sprintf("node hook PostVMStop vmi=%s/%s", ns, vmi.Name)))
	})
})
