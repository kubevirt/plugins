package main_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/iholder101/kubevirt-plugins/pkg/sdk/plugin"
	"github.com/iholder101/kubevirt-plugins/tests/framework"
)

var _ = Describe("Multi-Entrypoint Plugin", func() {
	It("should inject multiple sidecar containers from a single plugin", func() {
		ns := framework.TestContext.Namespace

		p := plugin.New("test-multi-entrypoint").
			WithDomainHook(plugin.ForLibvirt(&framework.NoopDomainHandler{}).WithEntrypoint("foo")).
			WithDomainHook(plugin.ForLibvirt(&framework.NoopDomainHandler{}).WithEntrypoint("bar")).
			WithNamespace(ns).
			WithImage(framework.TestContext.ContainerPrefix + "/multi_entrypoint:latest")

		framework.TestContext.DeployPlugin(p, ns)

		vmi := framework.TestContext.CreateVMI(ns)
		framework.TestContext.WaitForVMIRunning(ns, vmi.Name)

		pod := framework.TestContext.GetLauncherPod(ns, vmi.Name)
		Expect(framework.TestContext.CountSidecarContainers(pod)).To(BeNumerically(">=", 2),
			"launcher pod should have at least two sidecar containers (foo and bar entrypoints)")
	})
})
