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

// surrogateSpellings is one source's projection naming its subject in the first string and
// carrying, in the strings around it, the escapes a serialiser writes for the characters it
// holds: a pair, which is a character, and five code units with no pair, which every reader
// of this column reads as the replacement character. RFC 8259 admits all six, the module's
// acceptance rule at Save admits the document, and the column is text, so nothing above
// this file ever asks whether the escapes pair up.
const surrogateSpellings = `{"lead":"Wren Quill signed \ud800 here","high":"\ud800",` +
	`"pair":"\ud83d\ude00","two":"\ud800\ud800","low":"\udc00 tail","plain":"\ud800A",` +
	`"witness":"nobody"}`

// TestAnErasureReadsALoneSurrogateTheWayEveryReaderOfTheCopyDoes asks whether the erasure
// reaches the words a copy says about its subject when other strings of the same document
// are written in a spelling no surrogate pairs up into. The erasure is a document operation,
// so its reach is the reach of its reader, and the document outlives the erasure: the copy
// is read back by Get, List, Find and the keeper's personal-data export, all of which decode
// these escapes to the replacement character and carry on. So the rewrite has to read them
// the same way — the name goes, the escapes that name nobody come back byte for byte, and the
// copy stays one document — because the alternative is a copy that goes on naming the person
// in plain text while the erasure that was supposed to stop it reports the person forgotten.
func TestAnErasureReadsALoneSurrogateTheWayEveryReaderOfTheCopyDoes(t *testing.T) {
	_, conn := dbtest.Schema(t, saved.Migrations)
	grants := &desk{held: map[uuid.UUID][]string{}}
	keeper := uuid.New()
	grants.grant(keeper, contracts.PermissionRead, contracts.PermissionWrite)
	home := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	ctx := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), home), tenancy.Principal{UserID: keeper})
	ref := contracts.Ref{Module: "example", Kind: "thing", Key: "signed"}
	service := internal.NewService(grants, []contracts.Savable{
		{Module: "example", Kind: "thing", Source: &savedtest.Source{Value: contracts.Capture{
			Title: "Wren Quill signed", Locale: "en-GB", Version: "v1",
			Values: json.RawMessage(surrogateSpellings)}}},
	})
	subject := personaldata.Subject{ID: uuid.New(), DisplayName: "Wren Quill"}
	keep(t, ctx, conn, service, ref)
	erase(t, conn, ctx, subject)

	after := readAnswers(t, conn, ctx, service, ref)
	if !json.Valid(after.Snapshot.Values) {
		t.Fatalf("the erasure left a copy that is no longer a JSON object: %s", after.Snapshot.Values)
	}
	if _, err := json.Marshal(after); err != nil {
		t.Errorf("the library cannot answer with the copy after its erasure: %v", err)
	}
	for _, term := range subject.Identifiers() {
		if strings.Contains(strings.ToLower(string(after.Snapshot.Values)), strings.ToLower(term)) {
			t.Errorf("the erasure left %q standing in the copy: %s", term, after.Snapshot.Values)
		}
	}
	want := `{"lead":"` + subject.Token() + ` signed \ud800 here","high":"\ud800",` +
		`"pair":"\ud83d\ude00","two":"\ud800\ud800","low":"\udc00 tail","plain":"\ud800A",` +
		`"witness":"nobody"}`
	if string(after.Snapshot.Values) != want {
		t.Errorf("the erasure rewrote the copy as %s, want %s", after.Snapshot.Values, want)
	}
	if strings.Contains(after.Snapshot.Title, subject.DisplayName) {
		t.Errorf("the erasure left the name in the copy's title: %s", after.Snapshot.Title)
	}
}
