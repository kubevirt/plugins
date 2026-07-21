package framework

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (f *Framework) CollectArtifacts(namespace string) {
	if !CurrentSpecReport().Failed() {
		return
	}

	dir := filepath.Join(f.ArtifactsDir, namespace)
	os.MkdirAll(dir, 0755) //nolint:errcheck

	f.collectPodLogs(namespace, dir)
	f.collectEvents(namespace, dir)
}

func (f *Framework) collectPodLogs(namespace, dir string) {
	pods, err := f.KubeClient.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return
	}

	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			logs, err := f.KubeClient.CoreV1().Pods(namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
				Container: container.Name,
			}).DoRaw(context.Background())
			if err != nil {
				continue
			}

			filename := fmt.Sprintf("%s-%s.log", pod.Name, container.Name)
			os.WriteFile(filepath.Join(dir, filename), logs, 0644) //nolint:errcheck
		}
	}
}

func (f *Framework) collectEvents(namespace, dir string) {
	events, err := f.KubeClient.CoreV1().Events(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return
	}

	var data string
	for _, event := range events.Items {
		data += fmt.Sprintf("%s %s %s/%s: %s\n", event.LastTimestamp.Format("15:04:05"), event.Type, event.InvolvedObject.Kind, event.InvolvedObject.Name, event.Message)
	}

	os.WriteFile(filepath.Join(dir, "events.log"), []byte(data), 0644) //nolint:errcheck
}
