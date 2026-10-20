package internal_test

import (
	"context"
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

// TestMintingAnotherPersonsLinkRefusesAnActorWithoutTheRequiredGrant stands a second
// member at the operator's half of the mint — the call that is told it may skip the
// requester check — with a credential whose ceiling grants nothing, and asks for a link
// onto another person's ready bundle. The command has to refuse on its own, the way
// RequestExport and RequestErase do: no token handed out, no grant row committed, and
// the bundle's hold left where the walk put it.
func TestMintingAnotherPersonsLinkRefusesAnActorWithoutTheRequiredGrant(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, subject.Migrations)
	person := personaldata.Subject{ID: uuid.New(), Email: "private@example.com", DisplayName: "Private Person"}
	actor := personaldata.Subject{ID: uuid.New(), Email: "member@example.com", DisplayName: "Member"}
	seed(t, conn, person)
	seed(t, conn, actor)
	store := &bundlesStub{}
	svc := newDoor(&stamps{at: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)}, store, &notesStub{})
	id := ask(t, svc, conn, person)
	if err := walk(t, svc, conn, acme); err != nil {
		t.Fatalf("walk: %v", err)
	}
	if got := readRequest(t, conn, id); got.State != contracts.StateReady {
		t.Fatalf("the request is %s, want %s before anybody mints", got.State, contracts.StateReady)
	}
	holds := store.holds

	// An explicit empty credential ceiling grants nothing, whatever roles the member
	// may later be given: the authorizer would refuse this caller at the route, and
	// the command is asked whether it refuses them too.
	ctx := tenancy.WithPrincipal(tenancy.WithActor(tenancy.WithTenant(t.Context(), acme), actor.ID),
		tenancy.Principal{UserID: actor.ID, Permissions: []string{}})
	var link *contracts.Link
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		link, err = svc.Mint(ctx, tx, id, actor.ID, true)
		return err
	})
	if err == nil {
		t.Errorf("minting a link onto another person's bundle succeeded with an explicit empty permission ceiling")
	}
	if link != nil {
		t.Errorf("an unauthorised mint handed out a token onto request %s", link.Request)
	}
	var grants int64
	if err := transact(t, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Model(&contracts.Grant{}).Count(&grants).Error
	}); err != nil {
		t.Fatal(err)
	}
	if grants != 0 {
		t.Errorf("an unauthorised mint committed %d grant rows", grants)
	}
	if store.holds != holds {
		t.Errorf("an unauthorised mint moved the bundle's hold: %d holds, was %d", store.holds, holds)
	}
}
