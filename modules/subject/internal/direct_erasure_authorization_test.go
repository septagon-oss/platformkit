package internal_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/subject"
	"github.com/septagon-oss/platformkit/modules/subject/contracts"
	"github.com/septagon-oss/platformkit/modules/user"
)

func TestDirectErasureRefusesACredentialWithoutTheErasureGrant(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, subject.Migrations)
	person := personaldata.Subject{ID: uuid.New(), Email: "private@example.com", DisplayName: "Private Person"}
	actor := personaldata.Subject{ID: uuid.New(), Email: "member@example.com", DisplayName: "Member"}
	seed(t, conn, person)
	seed(t, conn, actor)
	rec := &recorder{}
	svc := newDoor(&stamps{at: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)}, &bundlesStub{}, &notesStub{}, rec)
	id := ask(t, svc, conn, person)
	ctx := tenancy.WithPrincipal(tenancy.WithActor(tenancy.WithTenant(t.Context(), acme), actor.ID),
		tenancy.Principal{UserID: actor.ID, Permissions: []string{}})
	var receipt *contracts.Erasure
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		receipt, err = svc.Erase(ctx, tx, contracts.EraseInput{Subject: person.ID, Confirm: person.Email})
		return err
	})
	if !errors.Is(err, contracts.ErrDenied) {
		t.Errorf("direct erasure with no grant = %v, want ErrDenied", err)
	}
	if receipt != nil {
		t.Errorf("refused erasure returned a receipt: %+v", receipt)
	}
	if len(rec.asks) != 0 {
		t.Errorf("refused erasure called contributors: %v", rec.asks)
	}
	if row := readRequest(t, conn, id); row.State != contracts.StateRequested || row.Revision != 1 {
		t.Errorf("refused erasure changed the export request: state=%s revision=%d", row.State, row.Revision)
	}
	if n := published(t, conn, contracts.EventErased); n != 0 {
		t.Errorf("refused erasure committed %d erased events", n)
	}
}
