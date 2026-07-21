package main_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/iholder101/kubevirt-plugins/pkg/sdk/plugin"
	"github.com/iholder101/kubevirt-plugins/tests/framework"
)

var _ = Describe("Domain Mutator", func() {
	It("should inject sidecar container and start VMI successfully", func() {
		ns := framework.TestContext.Namespace

		p := plugin.New("test-domain-mutator").
			WithDomainHook(plugin.ForLibvirt(&framework.NoopDomainHandler{})).
			WithNamespace(ns).
			WithImage(framework.TestContext.ContainerPrefix + "/domain_mutator:latest")

		framework.TestContext.DeployPlugin(p, ns)

		vmi := framework.TestContext.CreateVMI(ns)
		framework.TestContext.WaitForVMIRunning(ns, vmi.Name)

		pod := framework.TestContext.GetLauncherPod(ns, vmi.Name)
		Expect(framework.TestContext.CountSidecarContainers(pod)).To(BeNumerically(">=", 1),
			"launcher pod should have at least one sidecar container")
	})
})
