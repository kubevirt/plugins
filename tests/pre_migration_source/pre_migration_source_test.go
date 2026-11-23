package main

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubevirt/plugins/tests/framework"
)

var _ = Describe("Pre Migration Source", func() {
	requireTwoNodes := func() {
		if framework.TestContext.SchedulableNodeCount() < 2 {
			Skip("live migration needs two schedulable nodes, this cluster has fewer")
		}
	}

	It("should adjust the libvirt migration configuration on the source", func() {
		requireTwoNodes()
		ns := framework.TestContext.Namespace

		framework.TestContext.DeploySuitePlugin(newPlugin(), ns, "pre_migration_source")

		// A CPU limit keeps KubeVirt from configuring parallel migration
		// itself, so the hook's parallel connection count is what libvirt
		// ends up using.
		vmi := framework.TestContext.CreateVMI(ns, framework.WithCPULimit("2"))
		framework.TestContext.WaitForVMIRunning(ns, vmi.Name)

		sourcePod := framework.TestContext.GetLauncherPod(ns, vmi.Name)
		framework.TestContext.MigrateVMI(ns, vmi.Name)

		// The source launcher pod goes away once the migration succeeds, so
		// the sidecar's record of the migration configuration is checked
		// before waiting for the migration to complete. This proves the hook
		// was invoked with a decoded request and the migration still
		// succeeded with its response; the wire encoding of the requested
		// changes is unit-tested in pkg/sdk/plugin, and whether KubeVirt
		// applied the flags and params to libvirt is KubeVirt's own e2e
		// concern.
		Eventually(func() string {
			return framework.TestContext.ContainerLogs(ns, sourcePod.Name, "plugin-sidecar-test-pre-migration-source")
		}, 60*time.Second, 2*time.Second).Should(ContainSubstring("PreMigrationSource live=true p2p=true"))

		framework.TestContext.WaitForMigrationSucceeded(ns, vmi.Name)
	})

	It("should fail the migration when the hook errors", func() {
		requireTwoNodes()
		ns := framework.TestContext.Namespace

		framework.TestContext.DeploySuitePlugin(newPlugin(), ns, "pre_migration_source")

		vmi := framework.TestContext.CreateVMI(ns, framework.WithLabels(map[string]string{"sdk-pms-fail": "true"}))
		framework.TestContext.WaitForVMIRunning(ns, vmi.Name)

		framework.TestContext.MigrateVMI(ns, vmi.Name)

		Expect(framework.TestContext.WaitForMigrationFailed(ns, vmi.Name)).To(ContainSubstring("intentional PreMigrationSource failure"))
	})
})
