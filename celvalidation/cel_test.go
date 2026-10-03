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

package celvalidation

import (
	"context"
	"errors"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	namespace = "openperouter-system"
)

var _ = Describe("CEL Validation", func() {
	BeforeEach(func() {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		err := k8sClient.Create(context.Background(), ns)
		Expect(err).To(SatisfyAny(Not(HaveOccurred()), WithTransform(apierrors.IsAlreadyExists, BeTrue())))
	})

	AfterEach(func() {
		Expect(cleanupResources(k8sClient, namespace)).To(Succeed())
	})

	// This context verifies RouteTarget CEL rules by creating an L3VNI resources with a single
	// export route target for each test case. It then expects either success, or in case of failure it expects
	// a single failure cause to be present (meaning at max 1 CEL rule should ever report an error) with the
	// exact provided error string.
	Context("RouteTarget", func() {
		tcs := []struct {
			routeTarget     v1alpha1.RouteTarget
			wantErrorString string
		}{
			{
				routeTarget: v1alpha1.RouteTarget("123:10"),
			},
			{
				routeTarget: v1alpha1.RouteTarget("65535:65535"),
			},
			{
				routeTarget: v1alpha1.RouteTarget("0:0"),
			},
			{
				routeTarget: v1alpha1.RouteTarget("4294967295:100"),
			},
			{
				routeTarget: v1alpha1.RouteTarget("100:4294967295"),
			},
			{
				routeTarget: v1alpha1.RouteTarget("10.0.0.1:100"),
			},
			{
				routeTarget: v1alpha1.RouteTarget("192.168.1.1:65535"),
			},
			{
				routeTarget: v1alpha1.RouteTarget("0.0.0.0:0"),
			},
			{
				routeTarget: v1alpha1.RouteTarget("255.255.255.255:0"),
			},
			{
				routeTarget:     v1alpha1.RouteTarget("10.0.0.1"),
				wantErrorString: "Invalid value: \"string\": routeTarget must be in ASN:NN or IP:NN format",
			},
			{
				routeTarget:     v1alpha1.RouteTarget("100000:abc"),
				wantErrorString: "Invalid value: \"string\": routeTarget must be in ASN:NN or IP:NN format",
			},
			{
				routeTarget:     v1alpha1.RouteTarget("abc:100"),
				wantErrorString: "Invalid value: \"string\": routeTarget must be in ASN:NN or IP:NN format",
			},
			{
				routeTarget:     v1alpha1.RouteTarget(":100"),
				wantErrorString: "Invalid value: \"string\": routeTarget must be in ASN:NN or IP:NN format",
			},
			{
				routeTarget:     v1alpha1.RouteTarget("100:"),
				wantErrorString: "Invalid value: \"string\": routeTarget must be in ASN:NN or IP:NN format",
			},
			{
				routeTarget:     v1alpha1.RouteTarget("1000.0.0.1:100"),
				wantErrorString: "Invalid value: \"string\": routeTarget must be in ASN:NN or IP:NN format",
			},
			{
				routeTarget:     v1alpha1.RouteTarget("10.0.0:100"),
				wantErrorString: "Invalid value: \"string\": routeTarget must be in ASN:NN or IP:NN format",
			},
			{
				routeTarget:     v1alpha1.RouteTarget("4294967296:100"),
				wantErrorString: "Invalid value: \"string\": in ASN:NN format, ASN must not exceed 4294967295",
			},
			{
				routeTarget:     v1alpha1.RouteTarget("100:4294967296"),
				wantErrorString: "Invalid value: \"string\": in ASN:NN format, NN must not exceed 4294967295",
			},
			{
				routeTarget:     v1alpha1.RouteTarget("65536:65536"),
				wantErrorString: "Invalid value: \"string\": in ASN:NN format, either ASN or NN must be <= 65535",
			},
			{
				routeTarget:     v1alpha1.RouteTarget("10.0.0.1:65536"),
				wantErrorString: "Invalid value: \"string\": in IP:NN format, NN must be <= 65535",
			},
			// We'd need more complex validation to cover this, but then the regex's complexity would get out of hand.
			// Therefore, let CEL accept this and instead rely on the webhook for better validation.
			{
				routeTarget: v1alpha1.RouteTarget("300.300.300.300:100"),
			},
		}

		testCases := make([]testCase, len(tcs))
		for i, tc := range tcs {
			testCases[i] = testCase{
				name: fmt.Sprintf("route target: %s", tc.routeTarget),
				objects: []client.Object{
					&v1alpha1.L3VNI{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test",
							Namespace: namespace,
						},
						TypeMeta: metav1.TypeMeta{
							Kind:       "L3VNI",
							APIVersion: v1alpha1.GroupVersion.String(),
						},
						Spec: v1alpha1.L3VNISpec{
							VNI:       100,
							VRF:       "red",
							ExportRTs: []v1alpha1.RouteTarget{tc.routeTarget},
						},
					},
				},
			}
			if tc.wantErrorString != "" {
				testCases[i].wantCauses = []metav1.StatusCause{
					{
						Type:    "FieldValueInvalid",
						Message: tc.wantErrorString,
						Field:   "spec.exportRTs[0]",
					},
				}
			}
		}

		runTestCases(testCases)
	})
})

type testCase struct {
	name       string
	objects    []client.Object
	wantCauses []metav1.StatusCause
}

func runTestCases(testCases []testCase) {
	for _, tc := range testCases {
		Context(tc.name, func() {
			if len(tc.wantCauses) == 0 {
				It("should pass validation", func() {
					unstructuredObjects, err := objectsToUnstructured(tc.objects)
					Expect(err).NotTo(HaveOccurred())
					Expect(createObjects(k8sClient, unstructuredObjects...)).To(Succeed())
				})
				return
			}

			It("should fail validation", func() {
				unstructuredObjects, err := objectsToUnstructured(tc.objects)
				Expect(err).NotTo(HaveOccurred())
				err = createObjects(k8sClient, unstructuredObjects...)
				var statusErr *apierrors.StatusError
				Expect(errors.As(err, &statusErr)).To(BeTrue())
				Expect(statusErr.ErrStatus.Details).NotTo(BeNil())
				Expect(statusErr.ErrStatus.Details.Causes).To(Equal(tc.wantCauses))
			})
		})
	}
}

func objectsToUnstructured(objects []client.Object) ([]unstructured.Unstructured, error) {
	unstructuredObjects := make([]unstructured.Unstructured, len(objects))
	for i, obj := range objects {
		data, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
		if err != nil {
			return nil, fmt.Errorf("failed to convert to unstructured, err: %w", err)
		}
		unstructuredObjects[i] = unstructured.Unstructured{Object: data}
	}
	return unstructuredObjects, nil
}
