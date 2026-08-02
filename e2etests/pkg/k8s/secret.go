// SPDX-License-Identifier:Apache-2.0

package k8s

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

func CreateBasicAuthSecret(cs clientset.Interface, name, namespace, password string) error {
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Type: corev1.SecretTypeBasicAuth,
		Data: map[string][]byte{
			corev1.BasicAuthPasswordKey: []byte(password),
		},
	}
	_, err := cs.CoreV1().Secrets(namespace).Create(context.Background(), &secret, metav1.CreateOptions{})
	return err
}

func UpdateBasicAuthSecret(cs clientset.Interface, name, namespace, password string) error {
	secret, err := cs.CoreV1().Secrets(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	secret.Data[corev1.BasicAuthPasswordKey] = []byte(password)
	_, err = cs.CoreV1().Secrets(namespace).Update(context.Background(), secret, metav1.UpdateOptions{})
	return err
}

func RemoveSecret(cs clientset.Interface, name, namespace string) error {
	err := cs.CoreV1().Secrets(namespace).Delete(context.Background(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}
