package internal_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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

// unpairedSurrogate is a source's projection that names the person in plain words and,
// in a field that says nothing about anybody, carries the one escape a serialiser writes
// for a character it holds unpaired — a browser's JSON.stringify does this by default.
// The escape sits inside the grammar RFC 8259 allows, encoding/json decodes it to the
// replacement character rather than refusing it, and the module's own acceptance rule (a
// valid UTF-8 JSON object) admits the document it arrives in. The column is text, so no
// server-side jsonb read is left to ask the question either.
const unpairedSurrogate = `{"note":"signed by Wren Quill","raw":"\ud800"}`

// TestAnErasureReachesACopyWhoseDocumentHoldsALoneSurrogate asks whether an erasure
// reaches the words a copy says about its subject when some *other* string of the same
// document is written in a spelling the rewrite declines to read. The copy is a document
// and the rewrite is a document operation, so the reach of the erasure is the reach of
// its reader: a string it declines to decode has to decide either that the copy is not a
// snapshot at all, or that the erasure cannot certify it — and the answer has to be said
// out loud, because the alternative is an erasure that reports success with the person's
// name still standing in another person's list. So this case accepts any of the three
// honest answers and refuses only the silent one: the module refuses such a document at
// Save, or it refuses the erasure, or it rewrites the copy and the person's own strings
// are gone from it. What it may not do is keep the copy, take the erasure, and leave the
// name — which is also what README §7's "values as the document it is, rewritten inside
// the text its strings decode to" promises the copy will not be.
func TestAnErasureReachesACopyWhoseDocumentHoldsALoneSurrogate(t *testing.T) {
	_, conn := dbtest.Schema(t, saved.Migrations)
	grants := &desk{held: map[uuid.UUID][]string{}}
	keeper, subject := uuid.New(), personaldata.Subject{ID: uuid.New(), DisplayName: "Wren Quill"}
	grants.grant(keeper, contracts.PermissionRead, contracts.PermissionWrite)
	home := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	ownCtx := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), home), tenancy.Principal{UserID: keeper})
	ref := contracts.Ref{Module: "example", Kind: "thing", Key: "signed"}
	service := internal.NewService(grants, []contracts.Savable{
		{Module: "example", Kind: "thing", Source: &savedtest.Source{Value: contracts.Capture{
			Title: "Signed by Wren Quill", Locale: "en-GB", Version: "v1",
			Values: json.RawMessage(unpairedSurrogate)}}},
	})

	var kept contracts.Item
	err := db.Run(ownCtx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var e error
		kept, e = service.Save(ctx, tx, contracts.Save{Source: ref, ExpectedSourceVersion: "v1"})
		return e
	})
	if err != nil {
		// Answer one: the module refuses a snapshot whose document it could not rewrite
		// for the rest of the copy's life. Nothing was kept, so nothing can go on naming
		// anybody — provided the refusal is the malformed-snapshot one and not an accident.
		if !errors.Is(err, contracts.ErrInvalid) {
			t.Errorf("keeping a copy whose document holds an unpaired surrogate failed as %v, want the malformed-snapshot refusal", err)
		}
		return
	}

	if err = db.Run(ownCtx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return internal.PersonalData().Data.Erase(ctx, tx, subject)
	}); err != nil {
		// Answer two: an erasure that could not read a copy refuses to certify it, which is
		// what the kit's own JSON entry points do with a row they cannot parse.
		t.Logf("the erasure refused to certify the copy rather than miss it: %v", err)
		return
	}

	// Answer three: the copy survives, marked by the placeholder where the sentence about
	// the subject stood, and still reads as the document it was.
	after := readAnswers(t, conn, ownCtx, service, ref)
	if !json.Valid(after.Snapshot.Values) {
		t.Fatalf("the erasure left a copy that is no longer a JSON object: %s", after.Snapshot.Values)
	}
	for _, term := range subject.Identifiers() {
		if strings.Contains(strings.ToLower(string(after.Snapshot.Values)), strings.ToLower(term)) {
			t.Errorf("the erasure left %q standing in the copy, whose other string it would not read: %s", term, after.Snapshot.Values)
		}
	}
	if strings.Contains(after.Snapshot.Title, subject.DisplayName) {
		t.Errorf("the copy's title lost the name its values kept: %s / %s", after.Snapshot.Title, after.Snapshot.Values)
	}
	if !strings.Contains(string(after.Snapshot.Values), `"raw"`) {
		t.Errorf("the rewrite took the copy's own words with it: %s", after.Snapshot.Values)
	}
	if after.ID != kept.ID || after.Revision != kept.Revision {
		t.Errorf("the rewritten copy reads %+v, want the same copy it was", after.Item)
	}
}
