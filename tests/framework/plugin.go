package framework

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/kubevirt/plugins/pkg/sdk/plugin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (f *Framework) DeployPlugin(p *plugin.Plugin, namespace string) {
	GinkgoHelper()

	outputDir, err := os.MkdirTemp("", "plugin-deploy-")
	Expect(err).NotTo(HaveOccurred())

	// Generate also writes a Dockerfile and Makefile into the current
	// directory and reads the Go version from its go.mod; run it in a
	// scratch directory so the repository's own files are left alone.
	sourceDir, err := os.MkdirTemp("", "plugin-source-")
	Expect(err).NotTo(HaveOccurred())
	defer os.RemoveAll(sourceDir) //nolint:errcheck
	goMod, err := os.ReadFile(filepath.Join(f.RepoRoot, "go.mod"))
	Expect(err).NotTo(HaveOccurred())
	Expect(os.WriteFile(filepath.Join(sourceDir, "go.mod"), goMod, 0644)).To(Succeed())

	origDir, err := os.Getwd()
	Expect(err).NotTo(HaveOccurred())
	Expect(os.Chdir(sourceDir)).To(Succeed())
	defer os.Chdir(origDir) //nolint:errcheck

	Expect(p.Generate(outputDir)).To(Succeed())
	f.kubectlApply(outputDir, namespace)

	if hasMAP(outputDir) {
		f.waitForMAPReady()
	}

	DeferCleanup(func() {
		f.CollectArtifacts(namespace)
		f.UndeployPlugin(outputDir, namespace)
	})
}

// DeploySuitePlugin deploys a suite's plugin into ns with the image
// hack/cluster-build.sh builds for it: the container prefix plus the suite
// directory's name.
func (f *Framework) DeploySuitePlugin(p *plugin.Plugin, ns, suiteDir string) {
	GinkgoHelper()

	p.WithNamespace(ns).WithImage(f.ContainerPrefix + "/" + suiteDir + ":latest")
	f.DeployPlugin(p, ns)
}

func (f *Framework) UndeployPlugin(outputDir, namespace string) {
	f.kubectlDelete(outputDir, namespace)
	os.RemoveAll(outputDir) //nolint:errcheck
}

func (f *Framework) WaitForDaemonSetReady(namespace, name string) {
	EventuallyWithOffset(1, func() bool {
		daemonSet, err := f.KubeClient.AppsV1().DaemonSets(namespace).Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return false
		}
		return daemonSet.Status.DesiredNumberScheduled > 0 && daemonSet.Status.NumberReady == daemonSet.Status.DesiredNumberScheduled
	}, 120*time.Second, 5*time.Second).Should(BeTrue(), "DaemonSet %s/%s should be ready", namespace, name)
}

func (f *Framework) kubectlApply(dir, namespace string) {
	GinkgoHelper()

	entries, err := os.ReadDir(dir)
	Expect(err).NotTo(HaveOccurred())

	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		f.kubectl("apply", "-f", filepath.Join(dir, entry.Name()), "-n", namespace)
	}
}

func (f *Framework) kubectlCommand(args ...string) *exec.Cmd {
	return exec.CommandContext(context.Background(), f.Kubectl, append([]string{"--kubeconfig", f.Kubeconfig}, args...)...)
}

func (f *Framework) kubectlDelete(dir, namespace string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for i := len(entries) - 1; i >= 0; i-- {
		if filepath.Ext(entries[i].Name()) != ".yaml" {
			continue
		}
		cmd := f.kubectlCommand("delete", "-f", filepath.Join(dir, entries[i].Name()), "-n", namespace, "--ignore-not-found")
		cmd.CombinedOutput() //nolint:errcheck
	}
}

func (f *Framework) kubectl(args ...string) []byte {
	GinkgoHelper()

	output, err := f.kubectlCommand(args...).CombinedOutput()
	Expect(err).NotTo(HaveOccurred(), "kubectl %v failed: %s", args, string(output))
	return output
}

func hasMAP(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), "mutating-admission-policy") && !strings.Contains(entry.Name(), "binding") {
			return true
		}
	}
	return false
}

// waitForMAPReady waits for the API server to compile MutatingAdmissionPolicy
// CEL expressions. Compilation happens asynchronously after creation; pods
// created before it completes won't have mutations applied. There is no API
// to check compilation status, so we use a fixed delay.
func (f *Framework) waitForMAPReady() {
	time.Sleep(5 * time.Second)
}
