package main

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubevirt/plugins/tests/framework"
)

var _ = Describe("Multi-Entrypoint Plugin", func() {
	It("should apply every entrypoint's hook to the domain XML", func() {
		ns := framework.TestContext.Namespace

		p := newPlugin().WithNamespace(ns).WithImage(framework.TestContext.ContainerPrefix + "/multi_entrypoint:latest")
		framework.TestContext.DeployPlugin(p, ns)

		vmi := framework.TestContext.CreateVMI(ns)
		framework.TestContext.WaitForVMIRunning(ns, vmi.Name)

		// Hooks are applied in declaration order within the plugin, so foo
		// appends its marker first and bar joins with a comma.
		Expect(framework.TestContext.LauncherDomainXML(ns, vmi.Name)).To(ContainSubstring("<description>foo,bar</description>"))
	})
})
