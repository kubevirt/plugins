package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubevirt/plugins/tests/framework"
)

// fileIdentity is the identity of the cloud-init ISO as logged by the sidecar
// hook before and after the replacement.
type fileIdentity struct {
	kind    string
	path    string
	uid     string
	gid     string
	mode    string
	label   string
	ino     string
	mutated string
}

var identityRegexp = regexp.MustCompile(`PreBoot (original|replaced) path=(\S+) uid=(\d+) gid=(\d+) mode=(\d+) label=(\S+) ino=(\d+)(?: mutated=(\S+))?`)

func parseIdentities(logs string) []fileIdentity {
	matches := identityRegexp.FindAllStringSubmatch(logs, -1)
	identities := make([]fileIdentity, 0, len(matches))
	for _, match := range matches {
		identities = append(identities, fileIdentity{kind: match[1], path: match[2], uid: match[3], gid: match[4], mode: match[5], label: match[6], ino: match[7], mutated: match[8]})
	}
	return identities
}

// identityPair is the identity of the cloud-init ISO around one invocation of
// the hook.
type identityPair struct {
	original fileIdentity
	replaced fileIdentity
}

// pairIdentities pairs the identity lines in log order. PreBoot may repeat
// after a failed start, so the log can hold several pairs, and every original
// must be immediately followed by its replaced line.
func pairIdentities(identities []fileIdentity, logs string) []identityPair {
	var pairs []identityPair
	for i := 0; i < len(identities); i++ {
		Expect(identities[i].kind).To(Equal("original"), "identity lines must alternate original and replaced, logs:\n%s", logs)
		Expect(i+1).To(BeNumerically("<", len(identities)), "every original identity must be followed by its replaced line, logs:\n%s", logs)
		replaced := identities[i+1]
		Expect(replaced.kind).To(Equal("replaced"), "every original identity must be immediately followed by its replaced line, logs:\n%s", logs)
		Expect(replaced.mutated).To(BeElementOf("true", "false"), "replaced identity lines must say whether the invocation mutated the ISO, logs:\n%s", logs)
		pairs = append(pairs, identityPair{original: identities[i], replaced: replaced})
		i++
	}
	return pairs
}

var _ = Describe("PreBoot Artifact Replacement", func() {
	It("should replace the cloud-init ISO atomically and boot the VM from the replacement", func() {
		ns := framework.TestContext.Namespace

		framework.TestContext.DeploySuitePlugin(newPlugin(), ns, "pre_boot_artifact")

		userData := "#!/bin/sh\necho \"marker: " + markerBefore + "\" > /dev/console\n"
		vmi := framework.TestContext.CreateVMI(ns, framework.WithCloudInitNoCloud(userData), framework.WithSerialConsoleLog())
		framework.TestContext.WaitForVMIRunning(ns, vmi.Name)

		pod := framework.TestContext.GetLauncherPod(ns, vmi.Name)
		logs := framework.TestContext.ContainerLogs(ns, pod.Name, "plugin-sidecar-test-pre-boot-artifact")

		Expect(logs).NotTo(ContainSubstring("ReplaceFile error"), "sidecar log should not contain a ReplaceFile error, logs:\n%s", logs)

		pairs := pairIdentities(parseIdentities(logs), logs)
		Expect(pairs).NotTo(BeEmpty(), "sidecar log should contain at least one original and replaced identity pair, logs:\n%s", logs)

		// Only invocations that actually mutated the ISO anchor the checks:
		// the original of a no-op rerun is the already-replaced file. KubeVirt
		// regenerates the ISO before each PreBoot attempt, so retries
		// normally mutate again.
		var mutatedPairs []identityPair
		for _, pair := range pairs {
			if pair.replaced.mutated == "true" {
				mutatedPairs = append(mutatedPairs, pair)
			}
		}
		Expect(mutatedPairs).NotTo(BeEmpty(), "at least one PreBoot invocation must have mutated the ISO, logs:\n%s", logs)

		for _, pair := range mutatedPairs {
			By("Checking the sidecar swapped in a new inode with the original's ownership, mode and SELinux label")
			Expect(pair.replaced.path).To(Equal(pair.original.path), "the replacement should keep the path, original: %+v, replaced: %+v", pair.original, pair.replaced)
			Expect(pair.replaced.uid).To(Equal(pair.original.uid), "uid should be preserved, original: %+v, replaced: %+v", pair.original, pair.replaced)
			Expect(pair.replaced.gid).To(Equal(pair.original.gid), "gid should be preserved, original: %+v, replaced: %+v", pair.original, pair.replaced)
			Expect(pair.original.mode).NotTo(Equal("0600"), "the mode-restore proof needs a pristine mode different from the 0600 temp copy; KubeVirt creates the ISO 0644, original: %+v", pair.original)
			Expect(pair.replaced.mode).To(Equal(pair.original.mode), "the hook narrows the temp copy to 0600, so the original 0644 afterwards proves the helper restored the mode, original: %+v, replaced: %+v", pair.original, pair.replaced)
			Expect(pair.replaced.label).To(Equal(pair.original.label), "SELinux label should be preserved, original: %+v, replaced: %+v", pair.original, pair.replaced)
			Expect(pair.replaced.ino).NotTo(Equal(pair.original.ino), "the replacement should be a new inode (atomic rename), not an in-place rewrite, original: %+v, replaced: %+v", pair.original, pair.replaced)
		}

		// The pristine ISO identity is the original of the first mutating
		// invocation; the file on disk is the last invocation's replacement.
		pristine, onDisk := mutatedPairs[0].original, pairs[len(pairs)-1].replaced

		By("Checking the replaced ISO on disk in the compute container")
		isoContent := framework.TestContext.LauncherExec(ns, vmi.Name, "cat", pristine.path)
		Expect(isoContent).To(ContainSubstring(markerAfter), "the ISO should contain the mutated marker")
		Expect(isoContent).NotTo(ContainSubstring(markerBefore), "the ISO should not contain the original marker anymore")

		statOutput := strings.TrimSpace(framework.TestContext.LauncherExec(ns, vmi.Name, "stat", "-c", "%u:%g %a", pristine.path))
		mode, err := strconv.ParseUint(pristine.mode, 8, 32)
		Expect(err).NotTo(HaveOccurred())
		Expect(statOutput).To(Equal(pristine.uid+":"+pristine.gid+" "+fmt.Sprintf("%o", mode)), "on-disk ownership and mode should match the pristine identity: %+v", pristine)

		inodeOutput := strings.TrimSpace(framework.TestContext.LauncherExec(ns, vmi.Name, "stat", "-c", "%i", pristine.path))
		Expect(inodeOutput).To(Equal(onDisk.ino), "the on-disk inode should be the replacement's inode %s, not an older one", onDisk.ino)

		labelOutput := strings.TrimSpace(framework.TestContext.LauncherExec(ns, vmi.Name, "stat", "-c", "%C", pristine.path))
		if labelOutput != "?" {
			Expect(pristine.label).NotTo(Equal("<none>"), "SELinux is active in the compute container (stat -c %%C: %s), so the pristine ISO must have a label", labelOutput)
			Expect(labelOutput).To(Equal(pristine.label), "on-disk SELinux label should match the pristine identity: %+v", pristine)
		} else {
			By("Skipping the on-disk SELinux label check: the compute container reports no label (stat -c %C: ?)")
		}

		By("Checking the helper left no temporary files behind")
		entries := strings.Fields(framework.TestContext.LauncherExec(ns, vmi.Name, "ls", "-A", filepath.Dir(pristine.path)))
		for _, entry := range entries {
			Expect(entry).NotTo(ContainSubstring(".artifact-tmp-"), "helper temporary entry left behind: %s", entry)
		}

		By("Checking the guest booted from the replaced ISO")
		Eventually(func(g Gomega) {
			consoleLog, err := framework.TestContext.GuestConsoleLog(ns, pod.Name)
			g.Expect(err).NotTo(HaveOccurred(), "fetching the guest console log should succeed")
			g.Expect(consoleLog).To(ContainSubstring("marker: " + markerAfter))
		}, 5*time.Minute, 5*time.Second).Should(Succeed(), "guest console log should contain the mutated marker")
	})
})
