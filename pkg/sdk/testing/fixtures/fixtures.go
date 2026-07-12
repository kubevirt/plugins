package fixtures

import (
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "kubevirt.io/api/core/v1"
	"libvirt.org/go/libvirtxml"
)

func BasicLibvirtDomain() *libvirtxml.Domain {
	return &libvirtxml.Domain{
		Type:   "kvm",
		Name:   "test-domain",
		Memory: &libvirtxml.DomainMemory{Value: 1048576, Unit: "KiB"},
		VCPU:   &libvirtxml.DomainVCPU{Value: 2},
		OS: &libvirtxml.DomainOS{
			Type: &libvirtxml.DomainOSType{Type: "hvm"},
		},
	}
}

func BasicVMI() *v1.VirtualMachineInstance {
	return &v1.VirtualMachineInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-vmi",
			Namespace: "default",
		},
		Spec: v1.VirtualMachineInstanceSpec{
			Domain: v1.DomainSpec{
				Resources: v1.ResourceRequirements{
					Requests: k8sv1.ResourceList{
						k8sv1.ResourceMemory: resource.MustParse("1Gi"),
						k8sv1.ResourceCPU:    resource.MustParse("2"),
					},
				},
				CPU: &v1.CPU{Cores: 2},
			},
		},
	}
}
