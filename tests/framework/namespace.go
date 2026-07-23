package framework

import (
	"context"
	"fmt"
	"math/rand"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (f *Framework) createNamespace() string {
	name := fmt.Sprintf("test-plugins-%s", randomSuffix(6))
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}

	_, err := f.KubeClient.CoreV1().Namespaces().Create(context.Background(), namespace, metav1.CreateOptions{})
	ExpectWithOffset(2, err).NotTo(HaveOccurred(), "create namespace %s", name)

	return name
}

func (f *Framework) deleteNamespace(name string) {
	f.KubeClient.CoreV1().Namespaces().Delete(context.Background(), name, metav1.DeleteOptions{}) //nolint:errcheck
}

func randomSuffix(length int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	result := make([]byte, length)
	for i := range result {
		result[i] = chars[rand.Intn(len(chars))]
	}
	return string(result)
}
