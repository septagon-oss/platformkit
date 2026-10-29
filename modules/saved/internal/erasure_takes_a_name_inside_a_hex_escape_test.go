package internal_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/saved"
	"github.com/septagon-oss/platformkit/modules/saved/contracts"
	"github.com/septagon-oss/platformkit/modules/saved/contracts/savedtest"
	"github.com/septagon-oss/platformkit/modules/saved/internal"
)

// hexEscapedValues is a source's projection serialised by a writer that spells
// characters it does not print in \uXXXX form — what a Python json.dumps, a PHP
// json_encode or a Go encoder with HTML escaping on writes for a quotation mark. The
// name itself arrives as the bytes it is, so the server-side search the erasure asks
// first does select the copy; the escape sits in the same string, and the rewrite has
// to read it to know where the name ends. This is the only spelling of the JSON
// grammar no other case of this suite puts inside a string the erasure rewrites: the
// others escape only `"` and `\`, which the two-character escapes also carry.
const hexEscapedValues = `{"note":"she said \u0022Wren Quill\u0022 loudly",` +
	`"plain":"Wren Quill signed it","witness":"nobody named here"}`

// TestAnErasureTakesANameInsideAHexEscape pins the reach of a snapshot rewrite over the
// spellings a serialiser chooses. A retained copy is one document for its whole life, so
// the erasure reads each string as that document writes it and splices its placeholder
// over the bytes the matched text arrived as: a name the document put between two hex
// escapes is still the name, and the escapes around it are the copy's own words, which
// belong to the source and not to the person being forgotten. The copy must come back as
// one document with its untouched strings byte for byte — a rewrite that could not read
// an escape it was asked about, and so left the whole document standing, would report the
// person forgotten while their name is still readable in another person's list.
func TestAnErasureTakesANameInsideAHexEscape(t *testing.T) {
	_, conn := dbtest.Schema(t, saved.Migrations)
	grants := &desk{held: map[uuid.UUID][]string{}}
	keeper := uuid.New()
	grants.grant(keeper, contracts.PermissionRead, contracts.PermissionWrite)
	home := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	ctx := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), home), tenancy.Principal{UserID: keeper})
	ref := contracts.Ref{Module: "example", Kind: "thing", Key: "hex"}
	service := internal.NewService(grants, []contracts.Savable{
		{Module: "example", Kind: "thing", Source: &savedtest.Source{Value: contracts.Capture{
			Title: "Wren Quill signed", Locale: "en-GB", Version: "v1",
			Values: json.RawMessage(hexEscapedValues)}}},
	})
	subject := personaldata.Subject{ID: uuid.New(), DisplayName: "Wren Quill"}
	keep(t, ctx, conn, service, ref)
	erase(t, conn, ctx, subject)

	after := readAnswers(t, conn, ctx, service, ref)
	if !json.Valid(after.Snapshot.Values) {
		t.Fatalf("the erasure left a copy that is no longer a JSON object: %s", after.Snapshot.Values)
	}
	for _, term := range subject.Identifiers() {
		if strings.Contains(strings.ToLower(string(after.Snapshot.Values)), strings.ToLower(term)) {
			t.Errorf("the erasure left %q standing in the copy: %s", term, after.Snapshot.Values)
		}
	}
	if strings.Contains(after.Snapshot.Title, subject.DisplayName) {
		t.Errorf("the copy's title kept the name its values lost: %s", after.Snapshot.Title)
	}
	// The escape spellings the copy arrived with are the source's own bytes: the
	// rewrite takes the name between them and leaves them where they stood.
	want := `{"note":"she said \u0022` + subject.Token() + `\u0022 loudly",` +
		`"plain":"` + subject.Token() + ` signed it","witness":"nobody named here"}`
	if string(after.Snapshot.Values) != want {
		t.Errorf("the erasure rewrote the copy as %s, want %s", after.Snapshot.Values, want)
	}
}
