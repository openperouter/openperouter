// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"fmt"
	"net"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/url"
)

func checkPodIsReacheable(exec executor.Executor, from, to string) {
	GinkgoHelper()
	const port = "8090"
	hostPort := net.JoinHostPort(to, port)
	urlStr := url.Format("http://%s/clientip", hostPort)
	Eventually(func(g Gomega) string {
		By(fmt.Sprintf("trying to hit %s from %s", to, from))
		res, err := exec.Exec("curl", "-sS", urlStr)
		g.Expect(err).ToNot(HaveOccurred(), "curl %s failed: %s", hostPort, res)
		clientIP, _, err := net.SplitHostPort(res)
		g.Expect(err).ToNot(HaveOccurred())
		return clientIP
	}).
		WithTimeout(5*time.Second).
		WithPolling(time.Second).
		Should(Equal(from), "curl should return the expected clientip")
}

func canPingFromPod(exec executor.Executor, ip string) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		By(fmt.Sprintf("pinging %s via net1", ip))
		out, err := exec.Exec("ping", "-c", "1", "-W", "2", "-I", "net1", ip)
		g.Expect(err).ToNot(HaveOccurred(), "ping to %s failed: %s", ip, out)
	}).
		WithTimeout(40 * time.Second).
		WithPolling(time.Second).
		Should(Succeed())
}
