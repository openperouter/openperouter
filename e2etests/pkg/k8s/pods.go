// SPDX-License-Identifier:Apache-2.0

package k8s

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"time"

	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

func CreateAgnhostPod(cs clientset.Interface, podName, namespace string) (*corev1.Pod, error) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    "agnhost",
					Image:   "k8s.gcr.io/e2e-test-images/agnhost:2.40",
					Command: []string{"/agnhost"},
					Args: []string{
						"netexec",
						"--http-port=8090",
					},
					Ports: []corev1.ContainerPort{
						{
							Name:          "http",
							ContainerPort: 8090,
						},
					},
				},
			},
		},
	}
	pod, err := cs.CoreV1().Pods(namespace).Create(context.Background(), pod, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create pod %s: %w", podName, err)
	}
	if err := waitForPodReady(cs, pod); err != nil {
		return nil, err
	}
	return pod, nil
}

func waitForPodReady(cs clientset.Interface, pod *corev1.Pod) error {
	timeout := time.After(3 * time.Minute)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return fmt.Errorf("timed out waiting for pod %s to be ready", pod.Name)
		case <-ticker.C:
			toCheck, err := cs.CoreV1().Pods(pod.Namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
			if err != nil {
				break
			}
			if PodIsReady(toCheck) {
				return nil
			}
		}
	}
}

func PodLogsSinceTime(cs clientset.Interface, pod *corev1.Pod,
	speakerContainerName string, sinceTime *metav1.Time) (string, error) {
	podLogOpt := corev1.PodLogOptions{
		Container: speakerContainerName,
		SinceTime: sinceTime,
	}
	return PodLogs(cs, pod, podLogOpt)
}

func PodLogs(cs clientset.Interface, pod *corev1.Pod, podLogOpts corev1.PodLogOptions) (string, error) {
	req := cs.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &podLogOpts)
	podLogs, err := req.Stream(context.TODO())
	if err != nil {
		return "", err
	}
	defer func() {
		if err := podLogs.Close(); err != nil {
			panic("failed to close pod logs " + err.Error())
		}
	}()

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, podLogs)
	if err != nil {
		return "", err
	}

	str := buf.String()
	return str, nil
}

func NodeObjectForPod(cs clientset.Interface, pod *corev1.Pod) (*corev1.Node, error) {
	nodeName := pod.Spec.NodeName
	return cs.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
}

// PodIsReady returns the given pod's PodReady and ContainersReady condition.
func PodIsReady(p *corev1.Pod) bool {
	return podConditionStatus(p, corev1.PodReady) == corev1.ConditionTrue &&
		podConditionStatus(p, corev1.ContainersReady) == corev1.ConditionTrue
}

func PodsForLabel(cs clientset.Interface, namespace, labelSelector string) ([]*corev1.Pod, error) {
	pods, err := cs.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods with label %s: %w", labelSelector, err)
	}
	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("no pods found with label %s", labelSelector)
	}
	res := make([]*corev1.Pod, 0, len(pods.Items))
	for i := range pods.Items {
		res = append(res, &pods.Items[i])
	}
	return res, nil
}
func SendFileToPod(filePath string, p *corev1.Pod) error {
	dst := fmt.Sprintf("%s/%s:/", p.Namespace, p.Name)
	fullargs := []string{"cp", filePath, dst}
	_, err := exec.Command(executor.Kubectl, fullargs...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to send file %s to pod %s:%s: %w", filePath, p.Namespace, p.Name, err)
	}
	return nil
}

func NodeSelectorForPod(pod *corev1.Pod) map[string]string {
	if pod == nil {
		return nil
	}
	return map[string]string{
		"kubernetes.io/hostname": pod.Spec.NodeName,
	}
}

// podConditionStatus returns the status of the condition for a given pod.
func podConditionStatus(p *corev1.Pod, condition corev1.PodConditionType) corev1.ConditionStatus {
	if p == nil {
		return corev1.ConditionUnknown
	}

	for _, c := range p.Status.Conditions {
		if c.Type == condition {
			return c.Status
		}
	}

	return corev1.ConditionUnknown
}
