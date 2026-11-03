package framework

import (
	"flag"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var TestContext *Framework

var (
	suiteKubeconfig      string
	suiteKubectl         string
	suiteContainerPrefix string
	suiteArtifactsDir    string
)

type Framework struct {
	KubeClient      kubernetes.Interface
	DynamicClient   dynamic.Interface
	Kubeconfig      string
	Kubectl         string
	ContainerPrefix string
	ArtifactsDir    string
	RepoRoot        string
	Namespace       string
}

func SetupSuite() bool {
	flag.StringVar(&suiteKubeconfig, "kubeconfig", "", "path to kubeconfig file")
	flag.StringVar(&suiteKubectl, "kubectl", "kubectl", "kubectl binary used to apply generated plugin manifests")
	flag.StringVar(&suiteContainerPrefix, "container-prefix", "", "container image prefix for in-cluster references (e.g. registry:5000/kubevirt)")
	flag.StringVar(&suiteArtifactsDir, "artifacts", "_out/artifacts", "directory for test artifacts on failure")

	BeforeSuite(func() {
		Expect(suiteKubeconfig).NotTo(BeEmpty(), "-kubeconfig flag is required")
		Expect(suiteContainerPrefix).NotTo(BeEmpty(), "--container-prefix flag is required")
		initialize(suiteKubeconfig, suiteKubectl, suiteContainerPrefix, suiteArtifactsDir)
	})

	BeforeEach(func() {
		TestContext.Namespace = TestContext.createNamespace()
		DeferCleanup(TestContext.deleteNamespace, TestContext.Namespace)
	})

	return true
}

func initialize(kubeconfig, kubectl, containerPrefix, artifactsDir string) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		panic("failed to build kubeconfig: " + err.Error())
	}

	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic("failed to create kubernetes client: " + err.Error())
	}

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		panic("failed to create dynamic client: " + err.Error())
	}

	TestContext = &Framework{
		KubeClient:      kubeClient,
		DynamicClient:   dynamicClient,
		Kubeconfig:      kubeconfig,
		Kubectl:         kubectl,
		ContainerPrefix: containerPrefix,
		ArtifactsDir:    artifactsDir,
		RepoRoot:        discoverRepoRoot(),
	}
}

func discoverRepoRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		panic("failed to get working directory: " + err.Error())
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			panic("could not find repo root (no go.mod found in any parent directory)")
		}
		dir = parent
	}
}
