package cel_domain_hook_test

import (
	. "github.com/onsi/ginkgo/v2"

	"github.com/iholder101/kubevirt-plugins/pkg/sdk/plugin"
	"github.com/iholder101/kubevirt-plugins/tests/framework"
)

var _ = Describe("CEL Domain Hook", func() {
	It("should apply CEL domain hook and start VMI successfully", func() {
		ns := framework.TestContext.Namespace

		p := plugin.New("test-cel-domain-hook").
			WithDomainCELHook("Domain{Description: 'cel-functest'}").
			WithNamespace(ns)

		framework.TestContext.DeployPlugin(p, ns)

		vmi := framework.TestContext.CreateVMI(ns)
		framework.TestContext.WaitForVMIRunning(ns, vmi.Name)
	})
})
