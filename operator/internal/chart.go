/*


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

package operator

import (
	"github.com/openperouter/openperouter/internal/envconfig"
	operatorapi "github.com/openperouter/openperouter/operator/api/v1alpha1"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/getter"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// helmChart contains references which helps to
// to retrieve manifests from chart after patching given custom values.
type helmChart struct {
	client      *action.Install
	envSettings *cli.EnvSettings
	chart       *chart.Chart
}

// newChart initializes helm chart after loading it from given
// chart path and creating config object from environment variables.
// nolint:unparam
func newChart(chartPath, chartName, namespace string) (*helmChart, error) {
	chart := &helmChart{}
	chart.envSettings = cli.New()
	chart.client = action.NewInstall(new(action.Configuration))
	chart.client.ReleaseName = chartName
	chart.client.DryRun = true
	chart.client.ClientOnly = true
	chart.client.Namespace = namespace
	cp, err := chart.client.LocateChart(chartPath, chart.envSettings)
	if err != nil {
		return nil, err
	}
	chart.chart, err = loader.Load(cp)
	if err != nil {
		return nil, err
	}
	return chart, nil
}

// Objects retrieves manifests from chart after patching custom values passed in crdConfig
// and environment variables.
func (h *helmChart) Objects(envConfig envconfig.EnvConfig, crdConfig *operatorapi.OpenPERouter) ([]*unstructured.Unstructured, error) {
	chartValueOpts := &values.Options{}
	chartValues, err := chartValueOpts.MergeValues(getter.All(h.envSettings))
	if err != nil {
		return nil, err
	}

	patchChartValues(envConfig, crdConfig, chartValues)
	release, err := h.client.Run(h.chart, chartValues)
	if err != nil {
		return nil, err
	}
	objs, err := parseManifest(release.Manifest)
	if err != nil {
		return nil, err
	}
	for _, obj := range objs {
		// Set namespace explicitly into non cluster-scoped resource because helm doesn't
		// patch namespace into manifests at client.Run.
		obj.SetNamespace(envConfig.Namespace)
		if isServiceMonitor(obj) && envConfig.IsOpenshift {
			err := setOcpMonitorFields(obj)
			if err != nil {
				return nil, err
			}
		}
	}
	return objs, nil
}

func patchChartValues(envConfig envconfig.EnvConfig, crdConfig *operatorapi.OpenPERouter, valuesMap map[string]interface{}) {
	valuesMap["openperouter"] = map[string]interface{}{
		"logLevel": logLevelValue(crdConfig),
		"image": map[string]interface{}{
			"repository": envConfig.ControllerImage.Repo,
			"tag":        envConfig.ControllerImage.Tag,
		},
		"serviceAccounts": map[string]interface{}{
			"create": false,
			"controller": map[string]interface{}{
				"name": "controller",
			},
			"perouter": map[string]interface{}{
				"name": "default", // TODO: change this to perouter if/when that ServiceAccount gets actual RBACs attached.
			},
		},
		"frr": map[string]interface{}{
			"image": map[string]interface{}{
				"repository": envConfig.FRRImage.Repo,
				"tag":        envConfig.FRRImage.Tag,
			},
		},
		"crds": map[string]interface{}{
			"enabled": false,
		},
	}
}

func isServiceMonitor(obj *unstructured.Unstructured) bool {
	return obj.GetKind() == "ServiceMonitor"
}
