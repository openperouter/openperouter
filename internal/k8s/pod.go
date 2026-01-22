// SPDX-License-Identifier:Apache-2.0

package k8s

import (
	v1 "k8s.io/api/core/v1"
)

// PodIsReady returns the given pod's PodReady and ContainersReady condition.
func PodIsReady(p *v1.Pod) bool {
	return podConditionStatus(p, v1.PodReady) == v1.ConditionTrue && podConditionStatus(p, v1.ContainersReady) == v1.ConditionTrue
}

// podConditionStatus returns the status of the condition for a given pod.
func podConditionStatus(p *v1.Pod, condition v1.PodConditionType) v1.ConditionStatus {
	if p == nil {
		return v1.ConditionUnknown
	}

	for _, c := range p.Status.Conditions {
		if c.Type == condition {
			return c.Status
		}
	}

	return v1.ConditionUnknown
}
