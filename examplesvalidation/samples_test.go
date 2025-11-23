/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package examplesvalidation

import (
	"context"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Config Samples YAML Validation", func() {
	const namespace = "openperouter-system"

	BeforeEach(func() {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		err := k8sClient.Create(context.Background(), ns)
		if err != nil && !apierrors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred())
		}
	})

	AfterEach(func() {
		Expect(cleanupResources(k8sClient, namespace)).To(Succeed())
	})

	samplesDir := "../config/samples/"
	sampleFiles, err := discoverFiles(samplesDir, []string{".yaml", ".yml"})
	Expect(err).NotTo(HaveOccurred())
	for _, sampleFile := range sampleFiles {
		relPath, err := filepath.Rel(samplesDir, sampleFile)
		Expect(err).NotTo(HaveOccurred())

		// Skip kustomization.yaml files as they are not K8s resources
		if filepath.Base(sampleFile) == "kustomization.yaml" {
			continue
		}

		Context(relPath, func() {
			It("should successfully create resources from "+relPath, func() {
				Expect(validateResourceFromFile(k8sClient, relPath)).To(Succeed())
			})
		})
	}
})
