package cel_domain_hook_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/iholder101/kubevirt-plugins/tests/framework"
)

func TestCELDomainHook(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CEL Domain Hook Suite")
}

var _ = framework.SetupSuite()
