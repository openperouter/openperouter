// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openshift-kni/k8sreporter"
)

var (
	Updater     *config.Updater
	K8sReporter *k8sreporter.KubernetesReporter
	ReportPath  string
)
