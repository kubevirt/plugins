package framework

import (
	"context"
	"strings"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	v1 "kubevirt.io/api/core/v1"
)

var vmiGVR = schema.GroupVersionResource{
	Group:    "kubevirt.io",
	Version:  "v1",
	Resource: "virtualmachineinstances",
}

func (f *Framework) CreateVMI(namespace string) *v1.VirtualMachineInstance {
	vmi := &v1.VirtualMachineInstance{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "kubevirt.io/v1",
			Kind:       "VirtualMachineInstance",
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-",
			Namespace:    namespace,
		},
		Spec: v1.VirtualMachineInstanceSpec{
			Domain: v1.DomainSpec{
				Resources: v1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
				},
				Devices: v1.Devices{
					Disks: []v1.Disk{{
						Name: "containerdisk",
						DiskDevice: v1.DiskDevice{
							Disk: &v1.DiskTarget{Bus: v1.DiskBusVirtio},
						},
					}},
					Interfaces: []v1.Interface{{
						Name: "default",
						InterfaceBindingMethod: v1.InterfaceBindingMethod{
							Masquerade: &v1.InterfaceMasquerade{},
						},
					}},
				},
			},
			Networks: []v1.Network{{
				Name: "default",
				NetworkSource: v1.NetworkSource{
					Pod: &v1.PodNetwork{},
				},
			}},
			Volumes: []v1.Volume{{
				Name: "containerdisk",
				VolumeSource: v1.VolumeSource{
					ContainerDisk: &v1.ContainerDiskSource{
						Image: "quay.io/kubevirt/cirros-container-disk-demo",
					},
				},
			}},
		},
	}

	unstructuredMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(vmi)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	result, err := f.DynamicClient.Resource(vmiGVR).Namespace(namespace).Create(
		context.Background(), &unstructured.Unstructured{Object: unstructuredMap}, metav1.CreateOptions{})
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	createdVMI := &v1.VirtualMachineInstance{}
	ExpectWithOffset(1, runtime.DefaultUnstructuredConverter.FromUnstructured(result.Object, createdVMI)).To(Succeed())

	return createdVMI
}

func (f *Framework) WaitForVMIRunning(namespace, name string) {
	EventuallyWithOffset(1, func() v1.VirtualMachineInstancePhase {
		result, err := f.DynamicClient.Resource(vmiGVR).Namespace(namespace).Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return ""
		}
		vmi := &v1.VirtualMachineInstance{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(result.Object, vmi); err != nil {
			return ""
		}
		return vmi.Status.Phase
	}, 300*time.Second, 5*time.Second).Should(Equal(v1.Running), "VMI %s/%s should reach Running phase", namespace, name)
}

func (f *Framework) GetLauncherPod(namespace, vmiName string) *corev1.Pod {
	var launcherPod *corev1.Pod

	EventuallyWithOffset(1, func() bool {
		pods, err := f.KubeClient.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			return false
		}
		for i := range pods.Items {
			pod := &pods.Items[i]
			if strings.HasPrefix(pod.Name, "virt-launcher-"+vmiName) && pod.Status.Phase == corev1.PodRunning {
				launcherPod = pod
				return true
			}
		}
		return false
	}, 120*time.Second, 5*time.Second).Should(BeTrue(), "launcher pod for VMI %s/%s should be running", namespace, vmiName)

	return launcherPod
}

func (f *Framework) CountSidecarContainers(pod *corev1.Pod) int {
	count := 0
	for _, container := range pod.Spec.Containers {
		if container.Name == "compute" || strings.HasPrefix(container.Name, "volume") {
			continue
		}
		count++
	}
	return count
}
