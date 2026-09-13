package domain_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/septagon-oss/platformkit/modules/task/domain"
)

func TestResolutionDecisions(t *testing.T) {
	for _, tc := range []struct {
		name, status, recorded, requested string
		want                              domain.Resolution
		err                               error
	}{
		{name: "first resolution", status: "open", requested: "  valve replaced\n", want: domain.Resolution{Text: "valve replaced", Changed: true}},
		{name: "acknowledged", status: "acknowledged", requested: "done", want: domain.Resolution{Text: "done", Changed: true}},
		{name: "in progress", status: "in_progress", requested: "done", want: domain.Resolution{Text: "done", Changed: true}},
		{name: "empty first resolution", status: "open", requested: "\t\u2003", want: domain.Resolution{Changed: true}},
		{name: "legacy state", status: "legacy", requested: "done", want: domain.Resolution{Text: "done", Changed: true}},
		{name: "same text retry", status: "resolved", recorded: "valve replaced", requested: "valve replaced"},
		{name: "legacy whitespace retry", status: "resolved", recorded: "\u2003valve replaced\n", requested: "\tvalve replaced "},
		{name: "empty retry", status: "resolved", recorded: "valve replaced"},
		{name: "whitespace retry", status: "resolved", recorded: "valve replaced", requested: "\n\u2003\t"},
		{name: "different account", status: "resolved", recorded: "valve replaced", requested: "inspected only", err: domain.ErrDifferentResolution},
		{name: "case matters", status: "resolved", recorded: "done", requested: "Done", err: domain.ErrDifferentResolution},
		{name: "closed empty retry", status: "closed", recorded: "done", err: domain.ErrClosed},
		{name: "closed equal retry", status: "closed", recorded: "done", requested: "done", err: domain.ErrClosed},
		{name: "closed different request", status: "closed", recorded: "done", requested: "changed", err: domain.ErrClosed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := domain.Resolve(tc.status, tc.recorded, tc.requested)
			if !errors.Is(err, tc.err) || got != tc.want {
				t.Fatalf("Resolve = %+v, %v; want %+v, %v", got, err, tc.want, tc.err)
			}
		})
	}
}

func ExampleResolve() {
	decision, err := domain.Resolve(domain.StatusOpen, "", "  valve replaced  ")
	if err != nil {
		panic(err)
	}
	if decision.Changed {
		fmt.Println(domain.StatusResolved, decision.Text)
	}
	// Output: resolved valve replaced
}
