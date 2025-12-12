// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"fmt"
	"sort"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	"github.com/openperouter/openperouter/e2etests/pkg/status"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientset "k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Helper function to get RouterNodeConfigurationStatus resources
func getStatusList(k8sClient client.Client) *v1alpha1.RouterNodeConfigurationStatusList {
	statusList := &v1alpha1.RouterNodeConfigurationStatusList{}
	err := k8sClient.List(context.Background(), statusList, client.InNamespace(openperouter.Namespace))
	Expect(err).NotTo(HaveOccurred())
	return statusList
}

// Helper function to get nodes where the router controller daemonset is running
func getControllerNodes(k8sClient client.Client) []string {
	podList := &corev1.PodList{}
	err := k8sClient.List(context.Background(), podList,
		client.InNamespace(openperouter.Namespace),
		client.MatchingLabels{"app": "router"})
	Expect(err).NotTo(HaveOccurred())

	controllerNodes := make(map[string]bool)
	for _, pod := range podList.Items {
		controllerNodes[pod.Spec.NodeName] = true
	}

	var nodeNames []string
	for nodeName := range controllerNodes {
		nodeNames = append(nodeNames, nodeName)
	}
	return nodeNames
}

// Helper function to convert slice to set
func sliceToSet(slice []string) map[string]bool {
	set := make(map[string]bool)
	for _, item := range slice {
		set[item] = true
	}
	return set
}

// Helper function to perform set difference (A - B)
func setDifference(setA, setB map[string]bool) map[string]bool {
	result := make(map[string]bool)
	for item := range setA {
		if !setB[item] {
			result[item] = true
		}
	}
	return result
}

// Helper function to convert set to sorted slice
func setToSlice(set map[string]bool) []string {
	var slice []string
	for item := range set {
		slice = append(slice, item)
	}
	sort.Strings(slice)
	return slice
}

// Helper function to get stabilized RouterNodeConfigurationStatus
func getStabilizedStatusList(k8sClient client.Client) (*v1alpha1.RouterNodeConfigurationStatusList, error) {
	controllerNodes := getControllerNodes(k8sClient)
	statusList := getStatusList(k8sClient)

	if len(controllerNodes) == 0 {
		return nil, fmt.Errorf("expected at least one controller pod to be running")
	}

	if len(statusList.Items) != len(controllerNodes) {
		return nil, fmt.Errorf("expected %d RouterNodeConfigurationStatus resources (one per controller node), got %d",
			len(controllerNodes), len(statusList.Items))
	}

	return statusList, nil
}

var _ = Describe("RouterNodeConfigurationStatus CRD", func() {
	var cs clientset.Interface

	BeforeEach(func() {
		cs = k8sclient.New()
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		dumpIfFails(cs)
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())
	})

	Context("Lifecycle Management", func() {
		var k8sClient client.Client

		BeforeEach(func() {
			k8sClient = Updater.Client()
		})

		It("should automatically create RouterNodeConfigurationStatus resources with the expected name for each node with a running controller", func() {
			// Wait for RouterNodeConfigurationStatus resources to be created automatically
			// by the controller for nodes where the controller daemonset is scheduled
			Eventually(func() error {
				controllerNodes := getControllerNodes(k8sClient)
				statusList := getStatusList(k8sClient)

				if len(controllerNodes) == 0 {
					return fmt.Errorf("expected at least one controller pod to be running")
				}

				// Convert to sets
				controllerNodesSet := sliceToSet(controllerNodes)

				var resourceNames []string
				for _, status := range statusList.Items {
					resourceNames = append(resourceNames, status.Name)
				}
				resourceNamesSet := sliceToSet(resourceNames)

				// Find mismatches using set operations
				uncoveredNodes := setDifference(controllerNodesSet, resourceNamesSet)
				unmatchedResources := setDifference(resourceNamesSet, controllerNodesSet)

				// Build error message if there are mismatches
				var errorParts []string

				// Report missing RouterNodeConfigurationStatus resources
				if len(uncoveredNodes) > 0 {
					errorParts = append(errorParts, fmt.Sprintf("missing RouterNodeConfigurationStatus resources for controller nodes: %v", setToSlice(uncoveredNodes)))
				}

				// Report unmatched RouterNodeConfigurationStatus resources
				if len(unmatchedResources) > 0 {
					errorParts = append(errorParts, fmt.Sprintf("unmatched RouterNodeConfigurationStatus resources (no running controller): %v", setToSlice(unmatchedResources)))
				}

				// Fail if there are any mismatches
				if len(errorParts) > 0 {
					return fmt.Errorf(strings.Join(errorParts, "; "))
				}

				return nil
			}, "60s", "5s").Should(Succeed(), "RouterNodeConfigurationStatus resources should be created for nodes with running controllers")
		})

		It("should recreate RouterNodeConfigurationStatus resources if manually deleted", func() {
			var initialStatusList *v1alpha1.RouterNodeConfigurationStatusList

			Eventually(func() error {
				// Get stable status list with validation
				statusList, err := getStabilizedStatusList(k8sClient)
				if err != nil {
					return err
				}

				initialStatusList = statusList
				return nil
			}, "60s", "5s").Should(Succeed(), "Initial RouterNodeConfigurationStatus resources should be created")

			// Select resource to delete after Eventually completes
			resourceToDelete := &initialStatusList.Items[0] // Take the first resource for deletion

			// Store original properties for comparison
			originalName := resourceToDelete.Name
			originalNamespace := resourceToDelete.Namespace
			originalLastUpdateTime := resourceToDelete.Status.LastUpdateTime

			// Manually delete one RouterNodeConfigurationStatus resource
			err := k8sClient.Delete(context.Background(), resourceToDelete)
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to delete RouterNodeConfigurationStatus %s", originalName))

			// Verify the resource is recreated by the controller
			Eventually(func() error {
				recreatedResource := &v1alpha1.RouterNodeConfigurationStatus{}
				err := k8sClient.Get(context.Background(),
					types.NamespacedName{Name: originalName, Namespace: originalNamespace}, recreatedResource)
				if err != nil {
					return fmt.Errorf("RouterNodeConfigurationStatus %s should be recreated: %v", originalName, err)
				}

				// Verify status is initialized with a new timestamp
				if recreatedResource.Status.LastUpdateTime == nil {
					return fmt.Errorf("recreated RouterNodeConfigurationStatus %s should have lastUpdateTime set", recreatedResource.Name)
				}

				// Verify the timestamp is newer than the original (resource was actually recreated)
				if originalLastUpdateTime != nil && !recreatedResource.Status.LastUpdateTime.After(originalLastUpdateTime.Time) {
					return fmt.Errorf("recreated RouterNodeConfigurationStatus %s should have a newer timestamp", recreatedResource.Name)
				}

				return nil
			}, "60s", "5s").Should(Succeed(), "RouterNodeConfigurationStatus should be recreated with proper configuration")
		})

		It("should have proper owner references linking to Node resources", func() {
			// Validate RouterNodeConfigurationStatus resources have proper owner references to Node resources
			Eventually(func() error {
				// Get stable status list with validation
				statusList, err := getStabilizedStatusList(k8sClient)
				if err != nil {
					return err
				}

				for _, status := range statusList.Items {
					// Check owner references exist
					if len(status.OwnerReferences) == 0 {
						return fmt.Errorf("RouterNodeConfigurationStatus %s should have owner references", status.Name)
					}

					// Find the Node owner reference
					var nodeOwnerRef *metav1.OwnerReference
					for _, ownerRef := range status.OwnerReferences {
						if ownerRef.Kind == "Node" && ownerRef.APIVersion == "v1" {
							nodeOwnerRef = &ownerRef
							break
						}
					}

					if nodeOwnerRef == nil {
						return fmt.Errorf("RouterNodeConfigurationStatus %s should have Node owner reference", status.Name)
					}

					if nodeOwnerRef.Name != status.Name {
						return fmt.Errorf("Owner reference should point to node %s, got %s", status.Name, nodeOwnerRef.Name)
					}
				}

				return nil
			}, "60s", "5s").Should(Succeed(), "RouterNodeConfigurationStatus resources should have proper owner references")
		})

		It("should track multiple resource failures and recover properly", func() {
			// Step 1: Create an invalid underlay (nonexistent NIC)
			invalidUnderlay := v1alpha1.Underlay{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-underlay",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.UnderlaySpec{
					ASN:  64514,
					Nics: []string{"nonexistent"}, // Non-existent NIC
					EVPN: &v1alpha1.EVPNConfig{
						VTEPCIDR: "100.65.0.0/24",
					},
					Neighbors: []v1alpha1.Neighbor{
						{
							ASN:     64512,
							Address: "192.168.11.2",
						},
					},
				},
			}

			By("creating invalid underlay")
			err := Updater.Update(config.Resources{
				Underlays: []v1alpha1.Underlay{
					invalidUnderlay,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Step 2: Status failed
			By("confirming underlay status is failed")
			status.ExpectResourceFailure(k8sClient, "Underlay", invalidUnderlay.Name)

			// Step 3: Fix it
			fixedUnderlay := invalidUnderlay.DeepCopy()
			fixedUnderlay.Spec.Nics = []string{"toswitch"} // Use valid NIC

			By("fixing underlay")
			err = Updater.Update(config.Resources{
				Underlays: []v1alpha1.Underlay{
					*fixedUnderlay,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Step 4: Status OK
			By("confirming underlay status is now OK")
			status.ExpectSuccessfulStatus(k8sClient)

			// Step 5: Create an invalid L2VNI (nonexistent bridge)
			invalidL2VNI := v1alpha1.L2VNI{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-l2vni",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.L2VNISpec{
					VNI: 500,
					HostMaster: &v1alpha1.HostMaster{
						Name:       "nonexist-br", // Non-existent bridge - will fail at host setup
						Type:       "bridge",
						AutoCreate: false, // This will cause setup failure
					},
				},
			}

			By("creating invalid L2VNI referencing non-existent host bridge")
			err = Updater.Update(config.Resources{
				Underlays: []v1alpha1.Underlay{
					*fixedUnderlay,
				},
				L2VNIs: []v1alpha1.L2VNI{
					invalidL2VNI,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Step 6: Status failed
			By("confirming L2VNI status is failed")
			status.ExpectResourceFailure(k8sClient, "L2VNI", invalidL2VNI.Name)

			// Step 7: Remove it
			By("removing the failing L2VNI")
			err = k8sClient.Delete(context.Background(), &invalidL2VNI)
			Expect(err).NotTo(HaveOccurred())

			// Step 8: Status OK
			By("confirming status is OK after removing L2VNI")
			status.ExpectSuccessfulStatus(k8sClient)
		})
	})
})
