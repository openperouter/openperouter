// SPDX-License-Identifier:Apache-2.0

package openpeerrors

import (
	"errors"
	"fmt"

	"github.com/openperouter/openperouter/api/v1alpha1"
)

const (
	KindUnderlay      = v1alpha1.FailedResourceKind("Underlay")
	KindL3VNI         = v1alpha1.FailedResourceKind("L3VNI")
	KindL2VNI         = v1alpha1.FailedResourceKind("L2VNI")
	KindL3Passthrough = v1alpha1.FailedResourceKind("L3Passthrough")
)

// ResourceError represents a per-resource error (e.g., bad VRF name, duplicate VNI).
// These are reported in status.failedResources via CollectFailures.
type ResourceError struct {
	Obj v1alpha1.FailedResource
}

// Error implements the error interface so ResourceError can be used with errors.Join.
func (v *ResourceError) Error() string {
	return fmt.Sprintf("%s/%s: %s", v.Obj.Kind, v.Obj.Name, v.Obj.Message)
}

func CollectFailures(err error) []v1alpha1.FailedResource {
	var out []v1alpha1.FailedResource
	for _, e := range unwrapAll(err) {
		if ve, ok := e.(*ResourceError); ok {
			out = append(out, ve.Obj)
		}
	}
	return out
}

func IsNonResourceError(err error) bool {
	if err == nil {
		return false
	}
	var ve *ResourceError
	return !errors.As(err, &ve)
}

func HasUnderlayFailure(err error) bool {
	for _, e := range unwrapAll(err) {
		if ve, ok := e.(*ResourceError); ok && ve.Obj.Kind == KindUnderlay {
			return true
		}
	}
	return false
}

func IsUnderlayOnly(err error) bool {
	errs := unwrapAll(err)
	if len(errs) == 0 {
		return false
	}
	for _, e := range errs {
		ve, ok := e.(*ResourceError)
		if !ok || ve.Obj.Kind != KindUnderlay {
			return false
		}
	}
	return true
}

// unwrapAll flattens a (possibly nested) error tree into its leaf errors.
// It handles both errors.Join trees (Unwrap() []error) and single-wrapped
// errors (Unwrap() error), using an iterative stack to avoid recursion.
func unwrapAll(err error) []error {
	if err == nil {
		return nil
	}
	var out []error
	stack := []error{err}
	for len(stack) > 0 {
		n := len(stack) - 1
		e := stack[n]
		stack = stack[:n]
		if e == nil {
			continue
		}
		switch x := e.(type) {
		case interface{ Unwrap() []error }:
			subs := x.Unwrap()
			for i := len(subs) - 1; i >= 0; i-- {
				stack = append(stack, subs[i])
			}
		case interface{ Unwrap() error }:
			stack = append(stack, x.Unwrap())
		default:
			out = append(out, e)
		}
	}
	return out
}
