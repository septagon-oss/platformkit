package internal_test

import (
	"context"
	"encoding/json"
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

// quotingValues is a source's projection repeated by a retained copy, in the words the
// source wrote: the person's name inside quotation marks, after the backslashes of a
// path, as an @-mention, as an address and in capitals, and beside two numbers a reader
// of the copy expects to come back as they went in. A document had to escape some of
// those spellings to hold them at all.
const quotingValues = `{"note":"she said \"Wren Quill\" loudly","path":"C:\\Wren Quill\\notes",` +
	`"who":"@wren and wren@example.com","deep":{"again":"WREN QUILL"},"count":1.10,` +
	`"big":123456789012345678901234567890}`

// quietValues is what a copy says about nobody: a projection with none of the subject's
// strings in it, which an erasure therefore has no claim on.
const quietValues = `{"size":"red","note":"the source says nothing about anybody"}`

// quotedPerson is the person that projection quoted, known by an address, a name and a
// handle, which is every rule Subject.Known splits a person into.
func quotedPerson() personaldata.Subject {
	return personaldata.Subject{ID: uuid.New(), Email: "wren@example.com",
		DisplayName: "Wren Quill", Handle: "wren"}
}

// namesTheSubject and saysNothing are the two copies one keeper holds: the projection
// that quotes the person, and one that names nobody.
var (
	namesTheSubject = contracts.Ref{Module: "example", Kind: "thing", Key: "quoted"}
	saysNothing     = contracts.Ref{Module: "example", Kind: "quiet", Key: "quiet"}
)

// twoCopies signs in one keeper of both copies and answers the service, its connection
// and the context every command of the case runs in.
func twoCopies(t *testing.T) (*internal.Service, *db.Conn, context.Context) {
	t.Helper()
	_, conn := dbtest.Schema(t, saved.Migrations)
	grants := &desk{held: map[uuid.UUID][]string{}}
	keeper := uuid.New()
	grants.grant(keeper, contracts.PermissionRead, contracts.PermissionWrite)
	home := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	ctx := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), home), tenancy.Principal{UserID: keeper})
	service := internal.NewService(grants, []contracts.Savable{
		{Module: "example", Kind: "thing", Source: &savedtest.Source{Value: contracts.Capture{
			Title: "What Wren Quill asked for", Locale: "en-GB", Version: "v1",
			Values: json.RawMessage(quotingValues)}}},
		{Module: "example", Kind: "quiet", Source: &savedtest.Source{Value: contracts.Capture{
			Title: "A thing nobody is named in", Locale: "en-GB", Version: "v1",
			Values: json.RawMessage(quietValues)}}},
	})
	return service, conn, ctx
}

// keep returns the copy this person holds of one source, as the save answered it.
func keep(t *testing.T, ctx context.Context, conn *db.Conn, service *internal.Service,
	ref contracts.Ref) contracts.Item {
	t.Helper()
	var held contracts.Item
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		held, err = service.Save(ctx, tx, contracts.Save{Source: ref, ExpectedSourceVersion: "v1"})
		return err
	}); err != nil {
		t.Fatalf("keep a copy of %s/%s: %v", ref.Kind, ref.Key, err)
	}
	return held
}

// readAnswers answers with the copy, and with the error rather than an absence: a case
// that could not read its own fixture is not a case that found nothing.
func readAnswers(t *testing.T, conn *db.Conn, ctx context.Context, service *internal.Service,
	ref contracts.Ref) contracts.View {
	t.Helper()
	var held contracts.View
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		held, err = service.Find(ctx, tx, ref)
		return err
	}); err != nil {
		t.Fatalf("read the copy of %s/%s: %v", ref.Kind, ref.Key, err)
	}
	return held
}

func erase(t *testing.T, conn *db.Conn, ctx context.Context, subject personaldata.Subject) {
	t.Helper()
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return internal.PersonalData().Data.Erase(ctx, tx, subject)
	}); err != nil {
		t.Fatalf("erase %s: %v", subject.DisplayName, err)
	}
}

// TestAnErasureRewritesAQuotedNameAndKeepsTheDocument asks the question the snapshot
// column exists to answer: a retained copy is a JSON object for its whole life, and an
// erasure that reaches the words inside it must leave an object behind. Every spelling in
// the fixture is the same person — inside quotation marks, after a path's backslashes, as
// a mention, as an address, in capitals, and nested one level down — and the document
// escaped some of them to hold them, so the replacement has to land on the text each
// string decodes to rather than on the bytes around it. The numbers, the key order and
// the words around the placeholder are the keeper's own and come back byte for byte.
func TestAnErasureRewritesAQuotedNameAndKeepsTheDocument(t *testing.T) {
	service, conn, ownCtx := twoCopies(t)
	subject := quotedPerson()
	held := keep(t, ownCtx, conn, service, namesTheSubject)
	erase(t, conn, ownCtx, subject)

	after := readAnswers(t, conn, ownCtx, service, namesTheSubject)
	if !json.Valid(after.Snapshot.Values) {
		t.Fatalf("the erasure left a copy that is no longer a JSON object: %s", after.Snapshot.Values)
	}
	for _, term := range subject.Identifiers() {
		if strings.Contains(strings.ToLower(string(after.Snapshot.Values)), strings.ToLower(term)) {
			t.Errorf("the erasure left %q standing in the copy: %s", term, after.Snapshot.Values)
		}
	}
	token := subject.Token()
	want := `{"note":"she said \"` + token + `\" loudly","path":"C:\\` + token + `\\notes",` +
		`"who":"` + token + ` and ` + token + `","deep":{"again":"` + token + `"},"count":1.10,` +
		`"big":123456789012345678901234567890}`
	if string(after.Snapshot.Values) != want {
		t.Errorf("the erasure rewrote the copy as %s, want %s", after.Snapshot.Values, want)
	}
	if strings.Contains(after.Snapshot.Title, subject.DisplayName) {
		t.Errorf("the erasure left the name in the copy's title: %s", after.Snapshot.Title)
	}
	// The copy is the same copy: a rewrite of its words is not a new copy, and the
	// revision every unsave quotes did not move.
	if after.ID != held.ID || after.Revision != held.Revision || after.Source != held.Source {
		t.Errorf("the rewritten copy reads %+v, want the same copy of %v", after.Item, held.Source)
	}
	if !after.UpdatedAt.After(held.UpdatedAt) {
		t.Errorf("a copy whose words an erasure rewrote still says it last changed at %s", after.UpdatedAt)
	}
}

// TestAnErasureTakesANameTheDocumentEscaped asks the reach a rewrite of stored bytes cannot
// have. A person whose own name carries a quotation mark stands inside a document escaped,
// as John \"JJ\" Smith, and the erasure is asked of the directory's spelling — John "JJ"
// Smith — so reaching it means matching the text the document holds rather than the bytes
// around it. The title of the same copy is prose and takes the kit's scrub, which finds the
// directory's spelling there; the two paths have to agree about who was erased.
func TestAnErasureTakesANameTheDocumentEscaped(t *testing.T) {
	_, conn := dbtest.Schema(t, saved.Migrations)
	grants := &desk{held: map[uuid.UUID][]string{}}
	keeper, subject := uuid.New(), personaldata.Subject{ID: uuid.New(), DisplayName: `John "JJ" Smith`}
	grants.grant(keeper, contracts.PermissionRead, contracts.PermissionWrite)
	home := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	ownCtx := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), home), tenancy.Principal{UserID: keeper})
	ref := contracts.Ref{Module: "example", Kind: "thing", Key: "signed"}
	service := internal.NewService(grants, []contracts.Savable{
		{Module: "example", Kind: "thing", Source: &savedtest.Source{Value: contracts.Capture{
			Title: `Signed by John "JJ" Smith`, Locale: "en-GB", Version: "v1",
			Values: json.RawMessage(`{"note":"signed by John \"JJ\" Smith","witness":"nobody"}`)}}},
	})

	keep(t, ownCtx, conn, service, ref)
	erase(t, conn, ownCtx, subject)

	after := readAnswers(t, conn, ownCtx, service, ref)
	if !json.Valid(after.Snapshot.Values) {
		t.Fatalf("the erasure left a copy that is no longer a JSON object: %s", after.Snapshot.Values)
	}
	want := `{"note":"signed by ` + subject.Token() + `","witness":"nobody"}`
	if string(after.Snapshot.Values) != want {
		t.Errorf("the erasure left the name in the document: %s, want %s", after.Snapshot.Values, want)
	}
	if strings.Contains(after.Snapshot.Title, subject.DisplayName) {
		t.Errorf("the erasure left the name in the copy's title: %s", after.Snapshot.Title)
	}
}

// TestAnErasureWritesNothingToACopyThatNamesNobody bounds the same write from the other
// side: the person erased is somebody the copy says nothing about, so the copy is not
// touched — neither its bytes nor the instant it says it last changed. An erasure that
// rewrote a copy it had no claim on would take a keeper's snapshot for a word that was
// never in it.
func TestAnErasureWritesNothingToACopyThatNamesNobody(t *testing.T) {
	service, conn, ownCtx := twoCopies(t)
	kept := keep(t, ownCtx, conn, service, saysNothing)
	erase(t, conn, ownCtx, quotedPerson())

	after := readAnswers(t, conn, ownCtx, service, saysNothing)
	if string(after.Snapshot.Values) != quietValues {
		t.Errorf("an erasure rewrote a copy that named nobody: %s, want %s", after.Snapshot.Values, quietValues)
	}
	if !after.UpdatedAt.Equal(kept.UpdatedAt) {
		t.Errorf("an erasure moved the instant of a copy it had no claim on: %s, saved at %s",
			after.UpdatedAt, kept.UpdatedAt)
	}
}
