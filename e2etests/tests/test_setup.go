// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	clientset "k8s.io/client-go/kubernetes"
)

// SetupTestWithUnderlay initializes the test environment with an underlay configuration.
// It cleans all existing resources, creates a new Kubernetes client, gets routers,
// dumps router information, and applies the underlay configuration.
//
// Returns the routers and clientset for use in tests.
//
// Example usage:
//
//	BeforeAll(func() {
//	    cs, routers, err = SetupTestWithUnderlay()
//	    Expect(err).NotTo(HaveOccurred())
//	})
func SetupTestWithUnderlay() (clientset.Interface, openperouter.Routers, error) {
	err := Updater.CleanAll()
	if err != nil {
		return nil, nil, err
	}

	cs := k8sclient.New()
	routers, err := openperouter.Get(cs, HostMode)
	if err != nil {
		return nil, nil, err
	}

	routers.Dump(GinkgoWriter)

	err = Updater.Update(config.Resources{
		Underlays: []v1alpha1.Underlay{
			infra.Underlay,
		},
	})
	if err != nil {
		return nil, nil, err
	}

	return cs, routers, nil
}

// CleanupTestWithRolloutWait cleans all resources and waits for the router daemonset to rollout.
// This ensures the test environment is properly cleaned up after tests complete.
//
// Example usage:
//
//	AfterAll(func() {
//	    err := CleanupTestWithRolloutWait(cs, routers)
//	    Expect(err).NotTo(HaveOccurred())
//	})
func CleanupTestWithRolloutWait(cs clientset.Interface, originalRouters openperouter.Routers) error {
	err := Updater.CleanAll()
	if err != nil {
		return err
	}

	By("waiting for the router pod to rollout after removing the underlay")
	Eventually(func() error {
		newRouters, err := openperouter.Get(cs, HostMode)
		if err != nil {
			return err
		}
		return openperouter.DaemonsetRolled(originalRouters, newRouters)
	}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())

	return nil
}

// CleanupLeafPrefixes removes all prefixes from the specified leaves by calling
// removeLeafPrefixes on each leaf.
//
// Example usage:
//
//	AfterEach(func() {
//	    CleanupLeafPrefixes(infra.LeafAConfig, infra.LeafBConfig)
//	})
func CleanupLeafPrefixes(leaves ...infra.Leaf) {
	for _, leaf := range leaves {
		removeLeafPrefixes(leaf)
	}
}

// SetupLeafRedistributeConnected configures redistribute connected on all specified leaves.
//
// Example usage:
//
//	BeforeEach(func() {
//	    err := SetupLeafRedistributeConnected(infra.LeafAConfig, infra.LeafBConfig)
//	    Expect(err).NotTo(HaveOccurred())
//	})
func SetupLeafRedistributeConnected(leaves ...infra.Leaf) error {
	for _, leaf := range leaves {
		redistributeConnectedForLeaf(leaf)
	}
	return nil
}

// CleanupTestAfterEach performs standard cleanup after each test:
// - Dumps debug info if the test failed
// - Cleans all resources except the underlay
// - Removes leaf prefixes from all specified leaves
//
// Example usage:
//
//	AfterEach(func() {
//	    err := CleanupTestAfterEach(cs, infra.LeafAConfig, infra.LeafBConfig)
//	    Expect(err).NotTo(HaveOccurred())
//	})
func CleanupTestAfterEach(cs clientset.Interface, leaves ...infra.Leaf) error {
	dumpIfFails(cs)
	err := Updater.CleanButUnderlay()
	if err != nil {
		return err
	}
	CleanupLeafPrefixes(leaves...)
	return nil
}
