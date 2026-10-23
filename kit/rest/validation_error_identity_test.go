package rest_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/fault"
	"github.com/septagon-oss/platformkit/kit/rest"
)

func TestFaultPreservesValidationIdentity(t *testing.T) {
	t.Parallel()
	want := fault.Violations{
		{Location: "title", Code: "required", Message: "Choose a name"},
		{Location: "status", Code: "not_one_of", Message: "Choose an available option"},
	}
	err := fmt.Errorf("%w: %w", fault.ErrInvalid, want)
	mapped := rest.Fault(err)
	got, ok := errors.AsType[fault.Violations](mapped)
	if !ok || !slices.Equal(got, want) {
		t.Errorf("REST mapping lost errors.As identity: %v; got %+v, want %+v", mapped, got, want)
	}
	if !errors.Is(mapped, fault.ErrInvalid) {
		t.Errorf("REST mapping lost ErrInvalid: %v", mapped)
	}
}
