package main

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubevirt/plugins/pkg/sdk/plugin"
	"github.com/kubevirt/plugins/tests/framework"
)

var _ = Describe("Guest Definition", func() {
	It("should apply the sidecar GuestDefinition hook to the domain XML", func() {
		ns := framework.TestContext.Namespace

		p := newPlugin().WithNamespace(ns).WithImage(framework.TestContext.ContainerPrefix + "/guest_definition:latest")
		framework.TestContext.DeployPlugin(p, ns)

		vmi := framework.TestContext.CreateVMI(ns)
		framework.TestContext.WaitForVMIRunning(ns, vmi.Name)

		Expect(framework.TestContext.LauncherDomainXML(ns, vmi.Name)).To(ContainSubstring("<description>sdk-guest-definition</description>"))

		pod := framework.TestContext.GetLauncherPod(ns, vmi.Name)
		Expect(framework.TestContext.ContainerLogs(ns, pod.Name, "plugin-sidecar-test-guest-definition")).To(ContainSubstring("invocationContext=Boot"))
	})

	It("should inject the sidecar only into labelled virt-launcher pods", func() {
		ns := framework.TestContext.Namespace

		p := newGatedPlugin().WithNamespace(ns).WithImage(framework.TestContext.ContainerPrefix + "/guest_definition:latest")
		framework.TestContext.DeployPlugin(p, ns)

		labelled := framework.TestContext.CreateVMI(ns, framework.WithLabels(map[string]string{"sdk-sidecar-gate": "true"}))
		framework.TestContext.WaitForVMIRunning(ns, labelled.Name)
		Expect(framework.TestContext.LauncherDomainXML(ns, labelled.Name)).To(ContainSubstring("<description>sdk-guest-definition-gated</description>"))

		sidecarContainer := "plugin-sidecar-" + gatedPluginName
		labelledPod := framework.TestContext.GetLauncherPod(ns, labelled.Name)
		sidecar := framework.TestContext.ContainerByName(labelledPod, sidecarContainer)
		Expect(sidecar.Resources.Requests.Cpu().String()).To(Equal("50m"))
		Expect(sidecar.Resources.Requests.Memory().String()).To(Equal("64Mi"))
		Expect(framework.TestContext.ContainerLogs(ns, labelledPod.Name, sidecarContainer)).To(ContainSubstring("invocationContext=Boot"))

		unlabelled := framework.TestContext.CreateVMI(ns)
		framework.TestContext.WaitForVMIRunning(ns, unlabelled.Name)
		unlabelledPod := framework.TestContext.GetLauncherPod(ns, unlabelled.Name)
		Expect(framework.TestContext.CountSidecarContainers(unlabelledPod)).To(Equal(0))
		Expect(framework.TestContext.LauncherDomainXML(ns, unlabelled.Name)).NotTo(ContainSubstring("sdk-guest-definition-gated"))
	})

	It("should apply a CEL GuestDefinition hook only to matching VMIs", func() {
		ns := framework.TestContext.Namespace

		p := plugin.New("test-guest-definition-cel").
			WithLauncherHook(plugin.GuestDefinitionCEL(`Domain{Title: "sdk-cel"}`).WithCondition(`"sdk-cel" in vmi.Labels`)).
			WithNamespace(ns)
		framework.TestContext.DeployPlugin(p, ns)

		labelled := framework.TestContext.CreateVMI(ns, framework.WithLabels(map[string]string{"sdk-cel": "true"}))
		framework.TestContext.WaitForVMIRunning(ns, labelled.Name)
		Expect(framework.TestContext.LauncherDomainXML(ns, labelled.Name)).To(ContainSubstring("<title>sdk-cel</title>"))

		unlabelled := framework.TestContext.CreateVMI(ns)
		framework.TestContext.WaitForVMIRunning(ns, unlabelled.Name)
		Expect(framework.TestContext.LauncherDomainXML(ns, unlabelled.Name)).NotTo(ContainSubstring("<title>sdk-cel</title>"))
	})
})
