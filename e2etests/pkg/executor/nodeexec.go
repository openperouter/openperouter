// SPDX-License-Identifier:Apache-2.0

package executor

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
)

const (
	nodeExecHelperName  = "node-exec-helper"
	nodeExecHelperLabel = "app=" + nodeExecHelperName
)

var (
	forNodeClient    clientset.Interface
	forNodeNamespace string
)

func SetupNodeExec(cs clientset.Interface, namespace, image string) error {
	forNodeClient = cs
	forNodeNamespace = namespace

	if err := ensureServiceAccount(cs, namespace); err != nil {
		return fmt.Errorf("creating node-exec-helper service account: %w", err)
	}

	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nodeExecHelperName,
			Namespace: namespace,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": nodeExecHelperName},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": nodeExecHelperName},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: nodeExecHelperName,
					HostPID:            true,
					HostNetwork:        true,
					Tolerations:        []corev1.Toleration{{Operator: corev1.TolerationOpExists}},
					Containers: []corev1.Container{{
						Name:    "nsenter",
						Image:   image,
						Command: []string{"sleep", "infinity"},
						SecurityContext: &corev1.SecurityContext{
							Privileged: new(true),
						},
					}},
				},
			},
		},
	}

	_, err := cs.AppsV1().DaemonSets(namespace).Create(context.Background(), ds, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("creating node-exec-helper DaemonSet: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	err = wait.PollUntilContextCancel(ctx, 2*time.Second, true, func(ctx context.Context) (bool, error) {
		d, err := cs.AppsV1().DaemonSets(namespace).Get(ctx, nodeExecHelperName, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		if d.Status.DesiredNumberScheduled == 0 {
			return false, nil
		}
		return d.Status.NumberReady >= d.Status.DesiredNumberScheduled, nil
	})
	if err != nil {
		return fmt.Errorf("waiting for node-exec-helper pods to be ready: %w", err)
	}

	return nil
}

func TeardownNodeExec() error {
	if forNodeClient == nil {
		return nil
	}

	err := forNodeClient.AppsV1().DaemonSets(forNodeNamespace).Delete(
		context.Background(), nodeExecHelperName, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	err = forNodeClient.RbacV1().ClusterRoleBindings().Delete(
		context.Background(), nodeExecHelperName+"-privileged", metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	err = forNodeClient.RbacV1().ClusterRoles().Delete(
		context.Background(), nodeExecHelperName+"-privileged", metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	err = forNodeClient.CoreV1().ServiceAccounts(forNodeNamespace).Delete(
		context.Background(), nodeExecHelperName, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	return nil
}

type nodeExecutor struct {
	nodeName string
}

func ForNode(nodeName string) Executor {
	return &nodeExecutor{nodeName: nodeName}
}

func (e *nodeExecutor) Exec(cmd string, args ...string) (string, error) {
	if Kubectl == "" {
		return "", errors.New("the kubectl parameter is not set")
	}
	podName, err := helperPodForNode(e.nodeName)
	if err != nil {
		return "", err
	}
	nsenterArgs := []string{
		"exec", podName, "-n", forNodeNamespace, "-c", "nsenter", "--",
		"chroot", "/proc/1/root",
		"nsenter",
		"--target", "1",
		"--mount", "-u",
		"--ipc",
		"--net",
		cmd}
	fullargs := append(nsenterArgs, args...)
	out, err := exec.Command(Kubectl, fullargs...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("exec on node via pod %s/%s failed: %w. Output: %s",
			forNodeNamespace, podName, err, string(out))
	}
	return string(out), nil
}

func helperPodForNode(nodeName string) (string, error) {
	pods, err := forNodeClient.CoreV1().Pods(forNodeNamespace).List(
		context.Background(),
		metav1.ListOptions{LabelSelector: nodeExecHelperLabel},
	)
	if err != nil {
		return "", fmt.Errorf("failed to list helper pods: %w", err)
	}
	for i := range pods.Items {
		if pods.Items[i].Spec.NodeName == nodeName {
			return pods.Items[i].Name, nil
		}
	}
	return "", fmt.Errorf("no node-exec-helper pod found on node %s", nodeName)
}

func ensureServiceAccount(cs clientset.Interface, namespace string) error {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nodeExecHelperName,
			Namespace: namespace,
		},
	}
	_, err := cs.CoreV1().ServiceAccounts(namespace).Create(context.Background(), sa, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("creating service account: %w", err)
	}

	cr := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeExecHelperName + "-privileged",
		},
		Rules: []rbacv1.PolicyRule{{
			APIGroups:     []string{"security.openshift.io"},
			Resources:     []string{"securitycontextconstraints"},
			ResourceNames: []string{"privileged"},
			Verbs:         []string{"use"},
		}},
	}
	_, err = cs.RbacV1().ClusterRoles().Create(context.Background(), cr, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("creating cluster role: %w", err)
	}

	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeExecHelperName + "-privileged",
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     nodeExecHelperName + "-privileged",
		},
		Subjects: []rbacv1.Subject{{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      nodeExecHelperName,
			Namespace: namespace,
		}},
	}
	_, err = cs.RbacV1().ClusterRoleBindings().Create(context.Background(), crb, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("creating cluster role binding: %w", err)
	}

	return nil
}
