package fault_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/septagon-oss/platformkit/kit/fault"
)

func TestInvalidWithNoViolationsIsNil(t *testing.T) {
	t.Parallel()
	if err := fault.Invalid(); err != nil {
		t.Fatalf("Invalid() = %v, want nil", err)
	}
}

func TestViolationsAnswerInvalidAndSurviveWrapping(t *testing.T) {
	t.Parallel()
	err := fault.Invalid(
		fault.Violation{Location: "title", Code: "required", Message: "a task needs a title"},
		fault.Violation{Location: "status", Code: "not_one_of", Message: "bad status"},
	)
	wrapped := fmt.Errorf("%w: %w", fault.ErrInvalid, err)
	if !errors.Is(wrapped, fault.ErrInvalid) {
		t.Error("wrapped violations are not ErrInvalid")
	}
	got, ok := errors.AsType[fault.Violations](wrapped)
	if !ok || len(got) != 2 || got[0].Location != "title" || got[1].Location != "status" {
		t.Fatalf("violations = %v, %v", got, ok)
	}
	if want := "crud: invalid: a task needs a title; bad status"; wrapped.Error() != want {
		t.Errorf("text = %q, want %q", wrapped.Error(), want)
	}
}
