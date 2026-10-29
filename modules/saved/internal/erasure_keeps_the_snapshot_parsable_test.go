package internal_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/saved"
	"github.com/septagon-oss/platformkit/modules/saved/contracts"
	"github.com/septagon-oss/platformkit/modules/saved/contracts/savedtest"
	"github.com/septagon-oss/platformkit/modules/saved/internal"
)

// quotedKeeper signs in a keeper whose one copy holds a projection that quotes a
// second person, and returns the service, the tenant and the two person ids. The
// projection repeats the source's own words, quotation marks included, which is
// what a copy is for: it keeps what the source said, not a paraphrase.
func quotedKeeper(t *testing.T, capture contracts.Capture) (*internal.Service, *db.Conn, context.Context, uuid.UUID, personaldata.Subject) {
	t.Helper()
	_, conn := dbtest.Schema(t, saved.Migrations)
	grants := &desk{held: map[uuid.UUID][]string{}}
	keeper, named := uuid.New(), personaldata.Subject{ID: uuid.New(), DisplayName: `"Quill`}
	grants.grant(keeper, contracts.PermissionRead, contracts.PermissionWrite)
	home := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	ownCtx := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), home), tenancy.Principal{UserID: keeper})
	service := internal.NewService(grants, []contracts.Savable{{
		Module: "example", Kind: "thing", Source: &savedtest.Source{Value: capture}}})
	ref := contracts.Ref{Module: "example", Kind: "thing", Key: "quoted"}
	if err := db.Run(ownCtx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := service.Save(ctx, tx, contracts.Save{Source: ref, ExpectedSourceVersion: "v1"})
		return err
	}); err != nil {
		t.Fatalf("save the copy that quotes the person: %v", err)
	}
	return service, conn, ownCtx, keeper, named
}

// A retained snapshot is the contract's `Valid UTF-8 JSON object` for its whole
// life, and every reader of the library hands that object to an encoder: Get,
// List, Find and the personal-data export all marshal Item.Snapshot.Values. So an
// erasure that rewrites the snapshot's bytes as plain text owes the document the
// same answer a jsonb column would have asked for — that the words it leaves
// behind still parse. Here the name arrives inside a quoted phrase, so the bytes
// the match reaches end one quotation mark inside an escape the document had to
// write to hold that mark, and what is left where the name stood decides whether
// the copy can still be read at all.
func TestAnErasureLeavesTheRewrittenSnapshotParsable(t *testing.T) {
	named := json.RawMessage(`{"note":"he said \"Quill\" first","assignee":"Wren Quill"}`)
	service, conn, ownCtx, keeper, subject := quotedKeeper(t, contracts.Capture{
		Title: `What "Quill" asked for`, Locale: "en-GB", Version: "v1", Values: named})

	if err := db.Run(ownCtx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return internal.PersonalData().Data.Erase(ctx, tx, subject)
	}); err != nil {
		t.Fatalf("erase the person the copy quotes: %v", err)
	}

	var held contracts.View
	if err := db.Run(ownCtx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		held, err = service.Find(ctx, tx, contracts.Ref{Module: "example", Kind: "thing", Key: "quoted"})
		return err
	}); err != nil {
		t.Fatalf("read the copy the erasure rewrote: %v", err)
	}
	if !json.Valid(held.Snapshot.Values) {
		t.Errorf("the erasure left a snapshot that is no longer a JSON object: %s", held.Snapshot.Values)
	}
	if _, err := json.Marshal(held); err != nil {
		t.Errorf("the library cannot answer with the copy after its erasure: %v", err)
	}
	if _, err := json.Marshal([]contracts.View{held}); err != nil {
		t.Errorf("the person's list cannot answer after that erasure: %v", err)
	}
	if err := db.Run(ownCtx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := internal.PersonalData().Data.Export(ctx, tx, personaldata.Subject{ID: keeper})
		return err
	}); err != nil {
		t.Errorf("the keeper's personal-data export fails after an erasure of the person their copy quotes: %v", err)
	}
}
