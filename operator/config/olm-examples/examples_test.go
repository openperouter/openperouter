package examples

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"sigs.k8s.io/yaml"
)

func TestBundleIncludesOpenPERouterExample(t *testing.T) {
	sampleBytes, err := os.ReadFile("openperouter.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var sample map[string]any
	if err := yaml.Unmarshal(sampleBytes, &sample); err != nil {
		t.Fatal(err)
	}

	bundleBytes, err := os.ReadFile("../../bundle/manifests/openperouter-operator.clusterserviceversion.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var bundle struct {
		Metadata struct {
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
	}
	if err := yaml.Unmarshal(bundleBytes, &bundle); err != nil {
		t.Fatal(err)
	}
	var examples []map[string]any
	if err := json.Unmarshal([]byte(bundle.Metadata.Annotations["alm-examples"]), &examples); err != nil {
		t.Fatal(err)
	}
	for _, example := range examples {
		if example["apiVersion"] != sample["apiVersion"] || example["kind"] != sample["kind"] {
			continue
		}
		if !reflect.DeepEqual(example, sample) {
			t.Errorf("bundle example differs from source:\ngot:  %v\nwant: %v", example, sample)
		}
		return
	}
	t.Fatalf("bundle is missing example for %v %v", sample["apiVersion"], sample["kind"])
}
