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
	"bytes"
	"io"
	"strings"

	operatorapi "github.com/openperouter/openperouter/operator/api/v1alpha1"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func parseManifest(manifest string) ([]*unstructured.Unstructured, error) {
	rendered := bytes.Buffer{}
	rendered.Write([]byte(manifest))
	out := []*unstructured.Unstructured{}
	// special case - if the entire file is whitespace, skip
	if len(strings.TrimSpace(rendered.String())) == 0 {
		return out, nil
	}

	decoder := yaml.NewYAMLOrJSONDecoder(&rendered, 4096)
	for {
		u := unstructured.Unstructured{}
		if err := decoder.Decode(&u); err != nil {
			if err == io.EOF {
				break
			}
			return nil, errors.Wrapf(err, "failed to unmarshal manifest %s", manifest)
		}
		out = append(out, &u)
	}
	return out, nil
}

func logLevelValue(crdConfig *operatorapi.OpenPERouter) string {
	if crdConfig.Spec.LogLevel != "" {
		return string(crdConfig.Spec.LogLevel)
	}
	return string(operatorapi.LogLevelInfo)
}
