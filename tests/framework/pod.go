package framework

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ContainerLogs returns the current logs of one container of a pod.
func (f *Framework) ContainerLogs(namespace, podName, container string) string {
	logs, err := f.KubeClient.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{Container: container}).DoRaw(context.Background())
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "get logs of container %s in pod %s/%s", container, namespace, podName)
	return string(logs)
}

// ContainerByName returns the container with the given name, failing when absent.
func (f *Framework) ContainerByName(pod *corev1.Pod, name string) corev1.Container {
	for _, container := range pod.Spec.Containers {
		if container.Name == name {
			return container
		}
	}
	Fail(fmt.Sprintf("no container %s in pod %s/%s", name, pod.Namespace, pod.Name))
	return corev1.Container{}
}

// DaemonSetPodOnNode returns the pod of a DaemonSet that runs on nodeName.
func (f *Framework) DaemonSetPodOnNode(namespace, daemonSetName, nodeName string) *corev1.Pod {
	pods, err := f.KubeClient.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "app=" + daemonSetName})
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "list pods of DaemonSet %s/%s", namespace, daemonSetName)

	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.Spec.NodeName == nodeName {
			return pod
		}
	}
	Fail(fmt.Sprintf("no pod of DaemonSet %s/%s found on node %s", namespace, daemonSetName, nodeName))
	return nil
}

// SchedulableNodeCount returns the number of ready, schedulable nodes.
func (f *Framework) SchedulableNodeCount() int {
	nodes, err := f.KubeClient.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "list nodes")

	count := 0
	for i := range nodes.Items {
		if isNodeSchedulable(&nodes.Items[i]) {
			count++
		}
	}
	return count
}

func isNodeSchedulable(node *corev1.Node) bool {
	if node.Spec.Unschedulable {
		return false
	}
	for _, taint := range node.Spec.Taints {
		if taint.Effect == corev1.TaintEffectNoSchedule || taint.Effect == corev1.TaintEffectNoExecute {
			return false
		}
	}
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady && condition.Status != corev1.ConditionTrue {
			return false
		}
	}
	return true
}
