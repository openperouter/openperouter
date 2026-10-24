// SPDX-License-Identifier:Apache-2.0

package openpeerrors

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/openperouter/openperouter/api/v1alpha1"
)

func TestCollectFailures_nil(t *testing.T) {
	if got := CollectFailures(nil); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestCollectFailures_validationErrors(t *testing.T) {
	ve1 := newResourceError("L3VNI", "vni1", "bad vrf")
	ve2 := newResourceError("L2VNI", "vni2", "duplicate")

	got := CollectFailures(errors.Join(ve1, ve2))
	want := []v1alpha1.FailedResource{ve1.Obj, ve2.Obj}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestCollectFailures_underlayError(t *testing.T) {
	ue := newResourceError("Underlay", "my-underlay", "bad ASN")

	got := CollectFailures(ue)
	want := []v1alpha1.FailedResource{ue.Obj}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestCollectFailures_mixedErrors(t *testing.T) {
	ve := newResourceError("L3VNI", "vni1", "bad vrf")
	plain := fmt.Errorf("some other error")

	got := CollectFailures(errors.Join(ve, plain))
	want := []v1alpha1.FailedResource{ve.Obj}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestCollectFailures_nestedJoins(t *testing.T) {
	ve1 := newResourceError("L3VNI", "a", "err1")
	ve2 := newResourceError("L2VNI", "b", "err2")

	inner := errors.Join(ve1, ve2)
	outer := errors.Join(inner, fmt.Errorf("plain"))
	got := CollectFailures(outer)
	want := []v1alpha1.FailedResource{ve1.Obj, ve2.Obj}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestCollectFailures_noResourceErrors(t *testing.T) {
	got := CollectFailures(fmt.Errorf("other"))
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestCollectFailures_messageLength(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    string
	}{
		{name: "short", message: "bad config", want: "bad config"},
		{name: "at limit", message: strings.Repeat("a", 500), want: strings.Repeat("a", 500)},
		{name: "over limit", message: strings.Repeat("a", 501), want: strings.Repeat("a", 500)},
		{name: "multibyte at limit", message: strings.Repeat("界", 500), want: strings.Repeat("界", 500)},
		{name: "multibyte over limit", message: strings.Repeat("界", 501), want: strings.Repeat("界", 500)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := newResourceError("L3VNI", "vni1", tt.message)
			original := err.Obj
			originalError := err.Error()
			want := []v1alpha1.FailedResource{{
				Kind: "L3VNI", Name: "vni1",
				Reason: v1alpha1.FailedResourceReasonValidationFailed, Message: tt.want,
			}}
			if got := CollectFailures(err); !reflect.DeepEqual(got, want) {
				t.Errorf("got %+v, want %+v", got, want)
			}
			if err.Obj != original || err.Error() != originalError {
				t.Errorf("collection changed original error: got %+v, want %+v", err.Obj, original)
			}
		})
	}
}

func TestHasUnderlayFailure_nil(t *testing.T) {
	if HasUnderlayFailure(nil) {
		t.Error("nil should return false")
	}
}

func TestHasUnderlayFailure_true(t *testing.T) {
	ue := newResourceError("Underlay", "u1", "bad ASN")
	if !HasUnderlayFailure(ue) {
		t.Error("single underlay error should return true")
	}
}

func TestHasUnderlayFailure_mixed(t *testing.T) {
	ue := newResourceError("Underlay", "u1", "bad")
	ve := newResourceError("L3VNI", "vni1", "bad vrf")
	if !HasUnderlayFailure(errors.Join(ue, ve)) {
		t.Error("underlay + other validation should return true")
	}
}

func TestHasUnderlayFailure_noUnderlay(t *testing.T) {
	ve := newResourceError("L3VNI", "vni1", "bad vrf")
	if HasUnderlayFailure(ve) {
		t.Error("non-underlay error should return false")
	}
}

func TestHasUnderlayFailure_plainError(t *testing.T) {
	if HasUnderlayFailure(fmt.Errorf("something broke")) {
		t.Error("plain error should return false")
	}
}

func TestIsNonResourceError_nil(t *testing.T) {
	if IsNonResourceError(nil) {
		t.Error("nil should return false")
	}
}

func TestIsNonResourceError_validationError(t *testing.T) {
	ve := newResourceError("L3VNI", "vni1", "bad vrf")
	if IsNonResourceError(ve) {
		t.Error("ResourceError should return false")
	}
}

func TestIsNonResourceError_plainError(t *testing.T) {
	if !IsNonResourceError(fmt.Errorf("sysctl failed")) {
		t.Error("plain error should return true")
	}
}

func TestResourceError_errorString(t *testing.T) {
	ve := newResourceError("L3VNI", "test", "bad config")
	want := "L3VNI/test: bad config"
	if got := ve.Error(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func newResourceError(kind, name, message string) *ResourceError {
	return &ResourceError{
		Obj: v1alpha1.FailedResource{
			Kind: v1alpha1.FailedResourceKind(kind), Name: name,
			Reason: v1alpha1.FailedResourceReasonValidationFailed, Message: message,
		},
	}
}
