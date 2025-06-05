// SPDX-License-Identifier:Apache-2.0

package webhooks

import (
	"errors"

	"github.com/openperouter/openperouter/api/v1alpha1"
)

type mockValidator struct {
	vnis       []v1alpha1.VNI
	forceError bool
}

func (m *mockValidator) ValidateVNIs(vnis []v1alpha1.VNI) error {
	m.vnis = make([]v1alpha1.VNI, len(vnis))
	copy(m.vnis, vnis)

	if m.forceError {
		return errors.New("error!")
	}
	return nil
}
