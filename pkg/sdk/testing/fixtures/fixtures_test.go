package fixtures

import (
	"encoding/json"
	"encoding/xml"
	"testing"
)

func TestBasicLibvirtDomainIsValid(t *testing.T) {
	domain := BasicLibvirtDomain()

	if domain.Type != "kvm" {
		t.Errorf("expected type 'kvm', got %q", domain.Type)
	}

	if domain.Name != "test-domain" {
		t.Errorf("expected name 'test-domain', got %q", domain.Name)
	}

	if domain.Memory == nil {
		t.Fatal("expected memory to be set")
	}

	if domain.Memory.Value != 1048576 {
		t.Errorf("expected memory 1048576, got %d", domain.Memory.Value)
	}

	if domain.VCPU == nil {
		t.Fatal("expected vcpu to be set")
	}

	if domain.VCPU.Value != 2 {
		t.Errorf("expected vcpu 2, got %d", domain.VCPU.Value)
	}

	if domain.OS == nil || domain.OS.Type == nil {
		t.Fatal("expected OS type to be set")
	}

	if domain.OS.Type.Type != "hvm" {
		t.Errorf("expected OS type 'hvm', got %q", domain.OS.Type.Type)
	}

	data, err := xml.Marshal(domain)
	if err != nil {
		t.Fatalf("failed to marshal domain to XML: %v", err)
	}

	if len(data) == 0 {
		t.Error("expected non-empty XML output")
	}
}

func TestBasicVMIIsValid(t *testing.T) {
	vmi := BasicVMI()

	if vmi.Name != "test-vmi" {
		t.Errorf("expected name 'test-vmi', got %q", vmi.Name)
	}

	if vmi.Namespace != "default" {
		t.Errorf("expected namespace 'default', got %q", vmi.Namespace)
	}

	memReq := vmi.Spec.Domain.Resources.Requests["memory"]
	if memReq.String() != "1Gi" {
		t.Errorf("expected memory request '1Gi', got %q", memReq.String())
	}

	cpuReq := vmi.Spec.Domain.Resources.Requests["cpu"]
	if cpuReq.String() != "2" {
		t.Errorf("expected cpu request '2', got %q", cpuReq.String())
	}

	if vmi.Spec.Domain.CPU == nil {
		t.Fatal("expected CPU spec to be set")
	}

	if vmi.Spec.Domain.CPU.Cores != 2 {
		t.Errorf("expected 2 cores, got %d", vmi.Spec.Domain.CPU.Cores)
	}

	data, err := json.Marshal(vmi)
	if err != nil {
		t.Fatalf("failed to marshal VMI to JSON: %v", err)
	}

	if len(data) == 0 {
		t.Error("expected non-empty JSON output")
	}
}

func TestBasicLibvirtDomainReturnsFreshCopy(t *testing.T) {
	a := BasicLibvirtDomain()
	b := BasicLibvirtDomain()

	a.Name = "modified"

	if b.Name == "modified" {
		t.Error("modifying one copy affected the other - not returning fresh copies")
	}
}

func TestBasicVMIReturnsFreshCopy(t *testing.T) {
	a := BasicVMI()
	b := BasicVMI()

	a.Name = "modified"

	if b.Name == "modified" {
		t.Error("modifying one copy affected the other - not returning fresh copies")
	}
}
