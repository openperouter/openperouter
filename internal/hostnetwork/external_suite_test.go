// SPDX-License-Identifier:Apache-2.0

//go:build externaltests
// +build externaltests

package hostnetwork

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	paramsFile string
)

func init() {
	flag.StringVar(&paramsFile, "paramsfile", "", "the json file containing the parameters to verify")
	flag.Parse()
}

func TestHostNetwork(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "HostNetwork Suite")
}

var _ = Describe("EXTERNAL", func() {

	Context("underlay", func() {
		var params UnderlayParams

		BeforeEach(func() {
			var err error
			params, err = readParamsFromFile[UnderlayParams](paramsFile)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should be configured", func() {
			Eventually(func(g Gomega) {
				validateLoopback(g, params.Loopback.VtepIP)
				validateNIC(g, params.NIC.UnderlayInterface)
			}, 30*time.Second, 1*time.Second).Should(Succeed())
		})
	})

	Context("l3 vni", func() {
		var params L3VNIParams
		BeforeEach(func() {
			var err error
			params, err = readParamsFromFile[L3VNIParams](paramsFile)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should be configured", func() {
			Eventually(func(g Gomega) {
				validateL3VNI(g, params)
			}, 30*time.Second, 1*time.Second).Should(Succeed())
		})
		It("should be deleted", func() {
			Eventually(func(g Gomega) {
				validateVNIIsNotConfigured(g, params.VNIParams)
			}, 30*time.Second, 1*time.Second).Should(Succeed())
		})
	})

	Context("l2 vni", func() {
		var params L2VNIParams
		BeforeEach(func() {
			var err error
			params, err = readParamsFromFile[L2VNIParams](paramsFile)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should be configured", func() {
			Eventually(func(g Gomega) {
				validateL2VNI(g, params)
			}, 30*time.Second, 1*time.Second).Should(Succeed())
		})
		It("should be deleted", func() {
			Eventually(func(g Gomega) {
				validateVNIIsNotConfigured(g, params.VNIParams)
			}, 30*time.Second, 1*time.Second).Should(Succeed())
		})
	})

})

func readParamsFromFile[T any](filePath string) (T, error) {
	var params T

	file, err := os.Open(filePath)
	if err != nil {
		return params, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	byteValue, err := ioutil.ReadAll(file)
	if err != nil {
		return params, fmt.Errorf("failed to read file: %w", err)
	}

	err = json.Unmarshal(byteValue, &params)
	if err != nil {
		return params, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	return params, nil
}
