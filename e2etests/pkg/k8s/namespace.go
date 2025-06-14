// SPDX-License-Identifier:Apache-2.0

package k8s

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
)

func CreateNamespace(cs clientset.Interface, name string) (*corev1.Namespace, error) {
	_, err := cs.CoreV1().Namespaces().Create(context.Background(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create namespace %s: %w", name, err)
	}

	backoff := wait.Backoff{
		Duration: 1 * time.Second,
		Steps:    5,
	}

	var res *corev1.Namespace
	err = wait.ExponentialBackoff(backoff, func() (bool, error) {
		var err error
		res, err = cs.CoreV1().Namespaces().Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return true, nil
	})
	if err != nil {
		return nil, fmt.Errorf("namespace %s creation verification failed: %w", name, err)
	}

	return res, nil
}

func DeleteNamespace(cs clientset.Interface, name string) error {
	err := cs.CoreV1().Namespaces().Delete(context.Background(), name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete namespace %s: %w", name, err)
	}

	backoff := wait.Backoff{
		Duration: 1 * time.Second,
		Factor:   2,
		Steps:    10,
	}

	err = wait.ExponentialBackoff(backoff, func() (bool, error) {
		_, err := cs.CoreV1().Namespaces().Get(context.Background(), name, metav1.GetOptions{})
		if err == nil {
			return false, nil
		}
		if errors.IsNotFound(err) {
			return true, nil
		}
		return false, err // Other error
	})
	if err != nil {
		return fmt.Errorf("namespace %s deletion verification failed: %w", name, err)
	}

	return nil
}
