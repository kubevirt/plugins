package framework

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/iholder101/kubevirt-plugins/pkg/sdk/plugin"
	"libvirt.org/go/libvirtxml"
	v1 "kubevirt.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type NoopDomainHandler struct{}

func (*NoopDomainHandler) MutateDomain(_ context.Context, _ *libvirtxml.Domain, _ *v1.VirtualMachineInstance) error {
	return nil
}

type NoopNodeHandler struct{}

func (*NoopNodeHandler) ExecuteNodeHook(_ context.Context, _ *plugin.NodeHookRequest) error {
	return nil
}

func (f *Framework) DeployPlugin(p *plugin.Plugin, namespace string) {
	outputDir, err := os.MkdirTemp("", "plugin-deploy-")
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	origDir, err := os.Getwd()
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, os.Chdir(f.RepoRoot)).To(Succeed())
	defer os.Chdir(origDir) //nolint:errcheck

	ExpectWithOffset(1, p.Generate(outputDir)).To(Succeed())
	f.kubectlApply(outputDir, namespace)

	if hasMAP(outputDir) {
		f.waitForMAPReady(outputDir)
	}

	DeferCleanup(func() {
		f.CollectArtifacts(namespace)
		f.UndeployPlugin(outputDir, namespace)
	})
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
	entries, err := os.ReadDir(dir)
	ExpectWithOffset(2, err).NotTo(HaveOccurred())

	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		f.kubectl("apply", "-f", filepath.Join(dir, entry.Name()), "-n", namespace)
	}
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
		cmd := exec.CommandContext(context.Background(),
			filepath.Join(f.RepoRoot, "cluster", "kubectl.sh"),
			"delete", "-f", filepath.Join(dir, entries[i].Name()), "-n", namespace, "--ignore-not-found")
		cmd.Dir = f.RepoRoot
		cmd.CombinedOutput() //nolint:errcheck
	}
}

func (f *Framework) kubectl(args ...string) []byte {
	cmd := exec.CommandContext(context.Background(), filepath.Join(f.RepoRoot, "cluster", "kubectl.sh"), args...)
	cmd.Dir = f.RepoRoot
	output, err := cmd.CombinedOutput()
	ExpectWithOffset(2, err).NotTo(HaveOccurred(), "kubectl %v failed: %s", args, string(output))
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
func (f *Framework) waitForMAPReady(_ string) {
	time.Sleep(5 * time.Second)
}
