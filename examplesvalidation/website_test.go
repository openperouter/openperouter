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

var _ = Describe("Website YAML Validation", func() {
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

	contentDir := "../website"
	mdFiles, err := discoverFiles(contentDir, []string{".md"})
	Expect(err).NotTo(HaveOccurred())
	for _, mdFile := range mdFiles {
		yamlBlocks, err := extractYAMLFromMarkdown(mdFile)
		Expect(err).NotTo(HaveOccurred())
		if len(yamlBlocks) == 0 {
			continue
		}

		for idx, yamlBlock := range yamlBlocks {
			blockIdx := idx
			block := yamlBlock
			relPath, err := filepath.Rel(contentDir, mdFile)
			Expect(err).NotTo(HaveOccurred())

			Context(relPath, func() {
				It("should validate YAML block "+formatBlockNum(blockIdx), func() {
					Expect(validateResourceYAML(k8sClient, block)).To(Succeed(),
						"should contain valid openperouter CR at %s block %d", relPath, blockIdx+1)
				})
			})
		}
	}
})

// formatBlockNum formats the block number for test names (1-indexed)
func formatBlockNum(idx int) string {
	return string(rune('0' + idx + 1))
}
