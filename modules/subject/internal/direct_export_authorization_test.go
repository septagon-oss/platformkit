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

func TestDirectExportRefusesACredentialWithoutTheOtherPersonsExportGrant(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, subject.Migrations)
	person := personaldata.Subject{ID: uuid.New(), Email: "private@example.com", DisplayName: "Private Person"}
	actor := personaldata.Subject{ID: uuid.New(), Email: "member@example.com", DisplayName: "Member"}
	seed(t, conn, person)
	seed(t, conn, actor)
	rec := &recorder{}
	svc := newDoor(&stamps{at: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)}, &bundlesStub{}, &notesStub{}, rec)
	ctx := tenancy.WithPrincipal(tenancy.WithActor(tenancy.WithTenant(t.Context(), acme), actor.ID),
		tenancy.Principal{UserID: actor.ID, Permissions: []string{}})
	var bundle *contracts.Bundle
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		bundle, err = svc.Export(ctx, tx, person.ID)
		return err
	})
	if !errors.Is(err, contracts.ErrDenied) {
		t.Errorf("direct export of another person with no grant = %v, want ErrDenied", err)
	}
	if bundle != nil {
		t.Errorf("refused export returned %d bytes of another person's bundle", len(bundle.Zip))
	}
	if len(rec.asks) != 0 {
		t.Errorf("refused export called contributors: %v", rec.asks)
	}
}
