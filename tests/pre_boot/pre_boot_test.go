package main

import (
	"regexp"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "kubevirt.io/api/core/v1"

	"github.com/kubevirt/plugins/tests/framework"
)

var cloudInitISOSizeRegexp = regexp.MustCompile(`PreBoot cloudInitISO=\S+ size=(\d+)`)

var _ = Describe("Pre Boot", func() {
	It("should run after the cloud-init ISO was generated", func() {
		ns := framework.TestContext.Namespace

		p := newPlugin().WithNamespace(ns).WithImage(framework.TestContext.ContainerPrefix + "/pre_boot:latest")
		framework.TestContext.DeployPlugin(p, ns)

		vmi := framework.TestContext.CreateVMI(ns, framework.WithCloudInitNoCloud("#cloud-config\npassword: test\n"))
		framework.TestContext.WaitForVMIRunning(ns, vmi.Name)

		pod := framework.TestContext.GetLauncherPod(ns, vmi.Name)
		logs := framework.TestContext.ContainerLogs(ns, pod.Name, "plugin-sidecar-test-pre-boot")

		match := cloudInitISOSizeRegexp.FindStringSubmatch(logs)
		Expect(match).NotTo(BeNil(), "sidecar log should contain the PreBoot line, logs:\n%s", logs)
		size, err := strconv.Atoi(match[1])
		Expect(err).NotTo(HaveOccurred())
		Expect(size).To(BeNumerically(">", 0))
	})

	It("should keep the VMI from running when the hook fails with the Fail strategy", func() {
		ns := framework.TestContext.Namespace

		p := newPlugin().WithNamespace(ns).WithImage(framework.TestContext.ContainerPrefix + "/pre_boot:latest")
		framework.TestContext.DeployPlugin(p, ns)

		vmi := framework.TestContext.CreateVMI(ns, framework.WithLabels(map[string]string{"sdk-preboot-fail": "true"}))

		Eventually(func() string {
			return framework.TestContext.VMIConditionMessage(ns, vmi.Name, v1.VirtualMachineInstanceSynchronized)
		}, 2*time.Minute, 2*time.Second).Should(ContainSubstring("intentional PreBoot failure"))

		Expect(framework.TestContext.GetVMI(ns, vmi.Name).Status.Phase).NotTo(Equal(v1.Running))
	})
})
