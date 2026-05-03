/*
Copyright 2026.

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

package routerconfiguration

import (
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/openperouter/openperouter/api/v1alpha1"
)

type ReconcileResult struct {
	FailedResources []v1alpha1.FailedResource
	failedKinds     sets.Set[string]
}

func (r *ReconcileResult) AddFailure(kind, name string, reason v1alpha1.FailureReason, msg string) {
	r.FailedResources = append(r.FailedResources, v1alpha1.FailedResource{
		Kind:    kind,
		Name:    name,
		Reason:  reason,
		Message: msg,
	})
	if r.failedKinds == nil {
		r.failedKinds = sets.New[string]()
	}
	r.failedKinds.Insert(kind)
}

func (r *ReconcileResult) HasFailures() bool {
	return len(r.FailedResources) > 0
}

func (r *ReconcileResult) HasFailure(kind string) bool {
	return r.failedKinds.Has(kind)
}

func (r *ReconcileResult) Merge(other ReconcileResult) {
	r.FailedResources = append(r.FailedResources, other.FailedResources...)
	if r.failedKinds == nil {
		r.failedKinds = sets.New[string]()
	}
	r.failedKinds = r.failedKinds.Union(other.failedKinds)
}
