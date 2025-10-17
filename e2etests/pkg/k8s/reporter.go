// SPDX-License-Identifier:Apache-2.0

package k8s

import (
	"log"
	"strings"
	"time"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/onsi/ginkgo/v2"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openshift-kni/k8sreporter"
	"k8s.io/apimachinery/pkg/runtime"
)

func InitReporter(kubeconfig, path string, namespaces ...string) *k8sreporter.KubernetesReporter {
	// When using custom crds, we need to add them to the scheme
	addToScheme := func(s *runtime.Scheme) error {
		err := v1alpha1.AddToScheme(s)
		if err != nil {
			return err
		}
		err = frrk8sv1beta1.AddToScheme(s)
		if err != nil {
			return err
		}

		return nil
	}

	// The namespaces we want to dump resources for (including pods and pod logs)
	dumpNamespace := func(ns string) bool {
		for _, n := range namespaces {
			if n == ns {
				return true
			}
		}
		return false
	}

	// The list of CRDs we want to dump
	crds := []k8sreporter.CRData{
		{Cr: &v1alpha1.UnderlayList{}},
		{Cr: &v1alpha1.L3VNIList{}},
		{Cr: &v1alpha1.L2VNIList{}},
		{Cr: &frrk8sv1beta1.FRRConfigurationList{}},
	}

	reporter, err := k8sreporter.New(kubeconfig, addToScheme, dumpNamespace, path, crds...)
	if err != nil {
		log.Fatalf("Failed to initialize the reporter %s", err)
	}
	return reporter
}

func DumpInfo(reporter *k8sreporter.KubernetesReporter, testName string) {
	testNameNoSpaces := strings.ReplaceAll(ginkgo.CurrentSpecReport().LeafNodeText, " ", "-")
	reporter.Dump(10*time.Minute, testNameNoSpaces)
}
