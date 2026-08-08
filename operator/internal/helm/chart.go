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

package helm

import (
	"encoding/json"
	"fmt"

	operatorapi "github.com/openperouter/openperouter/operator/api/v1alpha1"
	"github.com/openperouter/openperouter/operator/internal/envconfig"
	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart"
	"helm.sh/helm/v4/pkg/chart/loader"
	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/release"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
)

const (
	// ContainerRuntimeContainerd is the containerd container runtime identifier
	ContainerRuntimeContainerd = "containerd"
	// ContainerRuntimeCrio is the CRI-O container runtime identifier
	ContainerRuntimeCrio = "crio"
)

// Chart contains references which helps to
// to retrieve manifests from chart after patching given custom values.
type Chart struct {
	client      *action.Install
	envSettings *cli.EnvSettings
	chart       chart.Charter
}

// NewChart initializes helm chart after loading it from given
// chart path and creating config object from environment variables.
// nolint:unparam
func NewChart(chartPath, chartName, namespace string) (*Chart, error) {
	chart := &Chart{}
	chart.envSettings = cli.New()
	chart.client = action.NewInstall(new(action.Configuration))
	chart.client.ReleaseName = chartName
	chart.client.DryRunStrategy = action.DryRunClient
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
func (h *Chart) Objects(envConfig envconfig.EnvConfig, crdConfig *operatorapi.OpenPERouter) ([]*unstructured.Unstructured, error) {
	chartValues := map[string]any{}
	if err := patchChartValues(envConfig, crdConfig, chartValues); err != nil {
		return nil, err
	}
	rel, err := h.client.Run(h.chart, chartValues)
	if err != nil {
		return nil, err
	}
	relAccessor, err := release.NewAccessor(rel)
	if err != nil {
		return nil, err
	}
	objs, err := parseManifest(relAccessor.Manifest())
	if err != nil {
		return nil, err
	}
	for _, obj := range objs {
		// Set namespace explicitly into non cluster-scoped resource because helm doesn't
		// patch namespace into manifests at client.Run.
		obj.SetNamespace(envConfig.Namespace)
	}
	return objs, nil
}

func patchChartValues(envConfig envconfig.EnvConfig, crdConfig *operatorapi.OpenPERouter, valuesMap map[string]any) error {
	cri := ContainerRuntimeContainerd
	if envConfig.IsOpenshift {
		cri = ContainerRuntimeCrio
	}
	openperouterValues := map[string]any{
		"logLevel": logLevelValue(crdConfig),
		"image": map[string]any{
			"repository": envConfig.ControllerImage.Repo,
			"tag":        envConfig.ControllerImage.Tag,
		},
		"serviceAccounts": map[string]any{
			"create": false,
			"controller": map[string]any{
				"name": "controller",
			},
			"nodemarker": map[string]any{
				"name": "nodemarker",
			},
			"perouter": map[string]any{
				"name": "perouter",
			},
		},
		"frr": map[string]any{
			"image": map[string]any{
				"repository": envConfig.FRRImage.Repo,
				"tag":        envConfig.FRRImage.Tag,
			},
		},
		"crds": map[string]any{
			"enabled": false,
		},
		"cri": cri,
	}

	// Only set nodeSelector/tolerations/affinity when explicitly provided on the
	// CRD, so an unset field falls back to the chart's own default (e.g., the
	// default tolerations for control-plane/master nodes).
	// The typed API structs are converted to plain map/slice values via JSON, so
	// the chart's values.schema.json (which only knows generic JSON types) can
	// validate them.
	if crdConfig.Spec.NodeSelector != nil {
		v, err := toJSONValue(crdConfig.Spec.NodeSelector)
		if err != nil {
			return fmt.Errorf("failed to convert nodeSelector to helm value: %w", err)
		}
		openperouterValues["nodeSelector"] = v
	}
	if crdConfig.Spec.Tolerations != nil {
		v, err := toJSONValue(crdConfig.Spec.Tolerations)
		if err != nil {
			return fmt.Errorf("failed to convert tolerations to helm value: %w", err)
		}
		openperouterValues["tolerations"] = v
	}
	if crdConfig.Spec.Affinity != nil {
		v, err := toJSONValue(crdConfig.Spec.Affinity)
		if err != nil {
			return fmt.Errorf("failed to convert affinity to helm value: %w", err)
		}
		openperouterValues["affinity"] = v
	}

	if crdConfig.Spec.OVSSocketPath != nil && *crdConfig.Spec.OVSSocketPath != "" {
		openperouterValues["ovsSocketPath"] = *crdConfig.Spec.OVSSocketPath
	}
	if crdConfig.Spec.OVSRunDir != nil && *crdConfig.Spec.OVSRunDir != "" {
		openperouterValues["ovsRunDir"] = *crdConfig.Spec.OVSRunDir
	}
	if crdConfig.Spec.HealthProbePort != nil && *crdConfig.Spec.HealthProbePort != 0 {
		openperouterValues["controller"] = map[string]any{
			"healthProbePort": *crdConfig.Spec.HealthProbePort,
		}
	}
	if crdConfig.Spec.BGPListenLimit != nil && *crdConfig.Spec.BGPListenLimit != 0 {
		openperouterValues["bgpListenLimit"] = *crdConfig.Spec.BGPListenLimit
	}

	datapath := ptr.Deref(crdConfig.Spec.Datapath, "kernel")
	openperouterValues["datapath"] = datapath
	if datapath == "grout" {
		groutImage := envConfig.GroutImage
		if groutImage == nil {
			groutImage = &envconfig.ImageInfo{
				Repo: "quay.io/openperouter/router",
				Tag:  "main-grout",
			}
		}

		openperouterValues["grout"] = map[string]any{
			"enabled": true,
			"image": map[string]any{
				"repository": groutImage.Repo,
				"tag":        groutImage.Tag,
			},
		}
	}

	valuesMap["openperouter"] = openperouterValues

	valuesMap["webhook"] = map[string]any{
		"enabled": false,
	}

	return nil
}

// toJSONValue converts a typed Go value (e.g. a Kubernetes API struct) into
// the plain map[string]any/[]any/etc. representation that Helm's value
// schema validation expects.
func toJSONValue(v any) (any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}
