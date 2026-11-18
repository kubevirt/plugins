package framework

import (
	"context"
	"time"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var migrationGVR = schema.GroupVersionResource{
	Group:    "kubevirt.io",
	Version:  "v1",
	Resource: "virtualmachineinstancemigrations",
}

// MigrateVMI creates a VirtualMachineInstanceMigration for the VMI and
// returns the migration's name.
func (f *Framework) MigrateVMI(namespace, name string) string {
	migration := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "kubevirt.io/v1",
			"kind":       "VirtualMachineInstanceMigration",
			"metadata": map[string]any{
				"generateName": "test-migration-",
				"namespace":    namespace,
			},
			"spec": map[string]any{"vmiName": name},
		},
	}

	result, err := f.DynamicClient.Resource(migrationGVR).Namespace(namespace).Create(context.Background(), migration, metav1.CreateOptions{})
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "create migration for VMI %s/%s", namespace, name)
	return result.GetName()
}

// WaitForMigrationSucceeded waits until the VMI's MigrationState reports a
// completed, non-failed migration.
func (f *Framework) WaitForMigrationSucceeded(namespace, vmiName string) {
	EventuallyWithOffset(1, func() bool {
		vmi, err := f.getVMI(namespace, vmiName)
		if err != nil || vmi.Status.MigrationState == nil {
			return false
		}
		return vmi.Status.MigrationState.Completed && !vmi.Status.MigrationState.Failed
	}, 300*time.Second, 5*time.Second).Should(BeTrue(), "migration of VMI %s/%s should succeed", namespace, vmiName)
}

// WaitForMigrationFailed waits until the VMI's MigrationState reports a failed
// migration and returns its FailureReason.
func (f *Framework) WaitForMigrationFailed(namespace, vmiName string) string {
	failureReason := ""
	EventuallyWithOffset(1, func() string {
		vmi, err := f.getVMI(namespace, vmiName)
		if err != nil || vmi.Status.MigrationState == nil || !vmi.Status.MigrationState.Failed {
			return ""
		}
		failureReason = vmi.Status.MigrationState.FailureReason
		return failureReason
	}, 300*time.Second, 5*time.Second).ShouldNot(BeEmpty(), "migration of VMI %s/%s should fail", namespace, vmiName)
	return failureReason
}
