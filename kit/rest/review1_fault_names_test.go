package rest_test

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/fault"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
)

// TestTheFaultNamesAnswerWhatTheCRUDNamesAnswer is the reviewer's pin for T-0031,
// asked from the consumer's side of the move. Every existing case of this mapping
// hands Fault a crud.Err… name, so nothing in the tree said the thing kit/fault
// exists for: that the value package which takes no transaction, and so wraps
// kit/fault, is answered the same status and the same detail as the adapter that
// wraps kit/crud. Declare the three again in kit/crud instead of aliasing them and
// each fault-named leg here fails, because the switch matches neither value, the
// refusal falls through the default branch and "no such row" answers 500. The
// detail is asserted beside the status because kit/rest/screens.go strips the
// literal "crud: invalid: " out of it: that text is a marker a second package
// parses, not inert wire data.
func TestTheFaultNamesAnswerWhatTheCRUDNamesAnswer(t *testing.T) {
	for _, tt := range []struct {
		status int
		detail string
		names  []error
	}{
		{http.StatusNotFound, "no such row, or none this tenant may see",
			[]error{fault.ErrNotFound, crud.ErrNotFound}},
		{http.StatusUnprocessableEntity, "crud: invalid: a title is required",
			[]error{fault.ErrInvalid, crud.ErrInvalid}},
		{http.StatusConflict, "crud: conflict: a title is required",
			[]error{fault.ErrConflict, crud.ErrConflict}},
	} {
		for _, name := range []string{"kit/fault", "kit/crud"} {
			err := tt.names[0]
			if name == "kit/crud" {
				err = tt.names[1]
			}
			got := rest.Fault(fmt.Errorf("%w: a title is required", err))
			p, ok := errors.AsType[*problem.Problem](got)
			if !ok || p.Status != tt.status || p.Detail != tt.detail {
				t.Errorf("Fault(a value package's wrap of %s's refusal) = %v; want %d %q",
					name, got, tt.status, tt.detail)
			}
		}
	}

	// What a duplicate reaches a client with is not the constraint name: the
	// message kit/crud builds for diagnostics stays off the wire, so the literal
	// pinned by kit/crud's TestClassifyMapsToTheSharedSentinels is what a log and
	// a wrapped ErrConflict carry, and never what a person is shown.
	unique := rest.Fault(&crud.UniqueConflict{Constraint: "internal_title_key"})
	p, ok := errors.AsType[*problem.Problem](unique)
	if !ok || p.Status != http.StatusConflict || strings.Contains(p.Detail, "internal_title_key") {
		t.Errorf("a unique violation reached the client as %v", unique)
	}
}
