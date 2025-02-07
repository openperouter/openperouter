// SPDX-License-Identifier:Apache-2.0

package hostnetwork

func deferWithError(errors *[]error, toDefer func() error) {
	if err := toDefer(); err != nil {
		*errors = append(*errors, err)
	}
}
