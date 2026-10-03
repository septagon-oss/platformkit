package crud_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/fault"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
)

// TestTheIsolationRefusalIsTheSharedValueUnderTheFaultName is the reviewer's pin
// for T-0031, asked where the two names meet a real refusal rather than a
// hand-made error. A value package wraps kit/fault because it takes no
// transaction; this adapter classifies what the database refused under
// kit/crud's name; one mapping in kit/rest answers both. So the refusal a
// second tenant gets at row-level security has to be matchable as
// fault.ErrNotFound and still answer 404 — not fall through to a 500 — because
// the caller that will read it may have written either name. Declare the three
// again in kit/crud instead of aliasing them and the first assertion fails,
// which is what no existing cross-tenant case can see: they all ask with a
// crud.Err… name.
func TestTheIsolationRefusalIsTheSharedValueUnderTheFaultName(t *testing.T) {
	conn := setup(t)
	mine := &Task{Title: "reviewed by a second tenant"}
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		if err := crud.Create(ctx, tx, mine); err != nil {
			t.Fatalf("Create: %v", err)
		}
	})
	attempt(t, conn, globex, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		_, err := crud.Get[*Task](tx, mine.ID)
		if !errors.Is(err, fault.ErrNotFound) {
			t.Errorf("a cross-tenant read refused with %v, which errors.Is does not match as fault.ErrNotFound", err)
		}
		p, ok := errors.AsType[*problem.Problem](rest.Fault(err))
		if !ok || p.Status != http.StatusNotFound {
			t.Errorf("rest.Fault answered that refusal %v, want a 404 problem", err)
		}
	})
	// The row is the owner's still, and reading it answers the same object.
	as(t, conn, acme, func(_ context.Context, tx db.Tx[db.Tenant]) {
		got, err := crud.Get[*Task](tx, mine.ID)
		if err != nil || got.Title != mine.Title {
			t.Errorf("the owner lost its row to the refusal: %v, %v", got, err)
		}
	})
}
