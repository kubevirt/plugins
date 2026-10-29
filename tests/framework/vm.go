package framework

import (
	"context"
	"strings"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

// libvirtSessionURI is the libvirt URI of a non-root virt-launcher, whose
// libvirt runs in the qemu user's session rather than the system instance.
const libvirtSessionURI = "qemu+unix:///session?socket=/var/run/libvirt/virtqemud-sock"

// VMIOption customizes a VMI created through CreateVMI.
type VMIOption func(*v1.VirtualMachineInstance)

// WithLabels sets labels on the VMI.
func WithLabels(labels map[string]string) VMIOption {
	return func(vmi *v1.VirtualMachineInstance) {
		if vmi.Labels == nil {
			vmi.Labels = map[string]string{}
		}
		for key, value := range labels {
			vmi.Labels[key] = value
		}
	}
}

// WithCloudInitNoCloud adds a cloudInitNoCloud volume with the given user
// data and a virtio disk backed by it.
func WithCloudInitNoCloud(userData string) VMIOption {
	return func(vmi *v1.VirtualMachineInstance) {
		vmi.Spec.Domain.Devices.Disks = append(vmi.Spec.Domain.Devices.Disks, v1.Disk{
			Name: "cloudinit",
			DiskDevice: v1.DiskDevice{
				Disk: &v1.DiskTarget{Bus: v1.DiskBusVirtio},
			},
		})
		vmi.Spec.Volumes = append(vmi.Spec.Volumes, v1.Volume{
			Name: "cloudinit",
			VolumeSource: v1.VolumeSource{
				CloudInitNoCloud: &v1.CloudInitNoCloudSource{UserData: userData},
			},
		})
	}
}

// WithCPULimit sets resources.limits.cpu, like KubeVirt's e2e WithCPULimit.
func WithCPULimit(limit string) VMIOption {
	return func(vmi *v1.VirtualMachineInstance) {
		if vmi.Spec.Domain.Resources.Limits == nil {
			vmi.Spec.Domain.Resources.Limits = corev1.ResourceList{}
		}
		vmi.Spec.Domain.Resources.Limits[corev1.ResourceCPU] = resource.MustParse(limit)
	}
}

func (f *Framework) CreateVMI(namespace string, opts ...VMIOption) *v1.VirtualMachineInstance {
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
	for _, opt := range opts {
		opt(vmi)
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

func (f *Framework) getVMI(namespace, name string) (*v1.VirtualMachineInstance, error) {
	result, err := f.DynamicClient.Resource(vmiGVR).Namespace(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	vmi := &v1.VirtualMachineInstance{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(result.Object, vmi); err != nil {
		return nil, err
	}
	return vmi, nil
}

// GetVMI returns the current state of the VMI.
func (f *Framework) GetVMI(namespace, name string) *v1.VirtualMachineInstance {
	vmi, err := f.getVMI(namespace, name)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "get VMI %s/%s", namespace, name)
	return vmi
}

// DeleteVMI deletes the VMI and waits until it is gone.
func (f *Framework) DeleteVMI(namespace, name string) {
	err := f.DynamicClient.Resource(vmiGVR).Namespace(namespace).Delete(context.Background(), name, metav1.DeleteOptions{})
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "delete VMI %s/%s", namespace, name)

	EventuallyWithOffset(1, func() bool {
		_, err := f.DynamicClient.Resource(vmiGVR).Namespace(namespace).Get(context.Background(), name, metav1.GetOptions{})
		return apierrors.IsNotFound(err)
	}, 180*time.Second, 2*time.Second).Should(BeTrue(), "VMI %s/%s should be deleted", namespace, name)
}

// VMIConditionMessage returns the message of the VMI's condition of the given
// type, or the empty string while the condition is not reported yet.
func (f *Framework) VMIConditionMessage(namespace, name string, conditionType v1.VirtualMachineInstanceConditionType) string {
	vmi, err := f.getVMI(namespace, name)
	if err != nil {
		return ""
	}
	for _, condition := range vmi.Status.Conditions {
		if condition.Type == conditionType {
			return condition.Message
		}
	}
	return ""
}

// WaitForVMIRunning waits until the VMI reaches the Running phase.
func (f *Framework) WaitForVMIRunning(namespace, name string) {
	EventuallyWithOffset(1, func() v1.VirtualMachineInstancePhase {
		vmi, err := f.getVMI(namespace, name)
		if err != nil {
			return ""
		}
		return vmi.Status.Phase
	}, 300*time.Second, 5*time.Second).Should(Equal(v1.Running), "VMI %s/%s should reach Running phase", namespace, name)
}

// LauncherDomainXML dumps the libvirt domain XML of a running VMI by running
// virsh dumpxml in the virt-launcher pod's compute container, like KubeVirt's
// own e2e tests. A non-root launcher runs libvirt in its session, which needs
// an explicit URI; a root launcher uses the system URI virsh defaults to.
func (f *Framework) LauncherDomainXML(namespace, vmiName string) string {
	vmi := f.GetVMI(namespace, vmiName)
	pod := f.GetLauncherPod(namespace, vmiName)

	command := []string{"virsh"}
	if vmi.Status.RuntimeUser != 0 {
		command = append(command, "-c", libvirtSessionURI)
	}
	command = append(command, "dumpxml", namespace+"_"+vmiName)

	output := f.kubectl(append([]string{"exec", "-n", namespace, pod.Name, "-c", "compute", "--"}, command...)...)
	return string(output)
}

// GetLauncherPod returns the virt-launcher pod of a VMI, waiting until it is running.
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

// CountSidecarContainers counts a pod's plugin sidecars, excluding the compute and volume containers.
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
