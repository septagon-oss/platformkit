package rest_test

// A guided session owns no table, so the module's read is the whole of the row and
// the mount is the only place that can notice when it answers a different session
// than the request named. Served, that answer is cached and shown under an id that
// is not the row's: a phone holding step two of a session it never opened.

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/rest"
)

const borrowedTitle = "Another person's sequence"

// TestAGuidedReadAnsweringAnotherSessionRefusesToServeIt serves the item route
// twice: once over a read that answers the session the path names, which is the
// row the person gets, and once over the same read with its identity replaced by
// another session's. The second is refused; the first is the reachability of the
// route the second is refused on.
func TestAGuidedReadAnsweringAnotherSessionRefusesToServeIt(t *testing.T) {
	address := "/api/v1/practice/sessions/" + guidedID.String()
	honest := newGuided(t)
	got := guidedCall(honest.router, "GET", address, "", "", "")
	if got.Code != 200 || !strings.Contains(got.Body.String(), "Practice sequence") {
		t.Fatalf("the session the request names: %d %s", got.Code, got.Body.String())
	}
	mismatched := newGuided(t, func(spec *rest.SessionSpec) {
		answers := spec.Session.Load
		spec.Session.Load = func(ctx context.Context, tx db.Tx[db.Tenant], actor, id uuid.UUID) (entity.GuidedSession, error) {
			out, err := answers(ctx, tx, actor, id)
			if err != nil {
				return out, err
			}
			out.ID = uuid.New()
			out.Title = borrowedTitle
			return out, nil
		}
	})
	refused := guidedCall(mismatched.router, "GET", address, "", "", "")
	if refused.Code < 400 {
		t.Fatalf("a read that answered another session was served: %d %s", refused.Code, refused.Body.String())
	}
	if strings.Contains(refused.Body.String(), borrowedTitle) {
		t.Errorf("the refused read still handed out the row it borrowed: %s", refused.Body.String())
	}
}
