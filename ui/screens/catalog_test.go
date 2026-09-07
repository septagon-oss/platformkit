package screens_test

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// publishBody is the argument of the golden file's row command, and the reason
// it has one: a command with an argument is a form, and a shell has to be told
// its shape the way it is told an entity's.
type publishBody struct {
	At string `json:"at,omitempty" doc:"When it goes out; now if left empty"`
}

// The golden file is the seam a native shell reads. It is committed here and
// copied verbatim into platformkit-mobile/testdata, whose parser test reads it;
// a change here that the shell cannot parse fails there. Regenerate with
//
//	UPDATE_GOLDEN=1 go test ./ui/screens -run TestCatalogGolden
func TestCatalogGolden(t *testing.T) {
	t.Parallel()
	tags := resource()
	tags.Entity, tags.Path = "tag", "/api/v1/note/tags"
	tags.Schema.Entity, tags.Schema.Path = "tag", "/api/v1/note/tags"
	tags.Immutable = nil
	// One command about a row and one about the collection, one with an
	// argument and one without: the four shapes a shell has to render, in the
	// document the shell parses.
	notes := resource()
	notes.Commands = []httpx.Command{
		{
			Verb: "publish", Summary: "Publish a note",
			Description: "Makes the note visible. Publishing a published note changes nothing.",
			Auth:        httpx.Permission("note:write"),
			Fields:      crud.FieldsOf(reflect.TypeFor[publishBody]()),
		},
		{
			Verb: "archive", Summary: "Archive every resolved note",
			Description: "Takes what is finished out of the way.",
			Collection:  true, Auth: httpx.Permission("note:write"),
		},
	}
	catalog := screens.Catalog{Resources: []screens.Entry{
		screens.Describe1(notes, true),
		screens.Describe1(tags, false),
	}}
	got, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	const golden = "testdata/catalog.json"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != string(got) {
		t.Fatalf("testdata/catalog.json is stale; run with UPDATE_GOLDEN=1.\n%s", got)
	}
	var back screens.Catalog
	if err := json.Unmarshal(got, &back); err != nil {
		t.Fatal(err)
	}
	if len(back.Resources) != 2 || back.Resources[0].Fields[0].Name != "id" || !back.Resources[0].Fields[0].ReadOnly {
		t.Fatalf("the catalog does not round-trip: %+v", back)
	}
	if !back.Resources[0].Writable || back.Resources[1].Writable {
		t.Fatal("writable did not survive the trip")
	}
	if got := back.Resources[0].Immutable; len(got) != 1 || got[0] != "status" {
		t.Fatalf("immutable = %v", got)
	}
	cmds := back.Resources[0].Commands
	if len(cmds) != 2 || cmds[0].Verb != "publish" || !cmds[1].Collection {
		t.Fatalf("the commands did not survive the trip: %+v", cmds)
	}
	if len(cmds[0].Fields) != 1 || cmds[0].Fields[0].Name != "at" || len(cmds[1].Fields) != 0 {
		t.Fatalf("a command's argument did not survive the trip: %+v", cmds[0].Fields)
	}
	if len(back.Resources[1].Commands) != 0 {
		t.Fatal("a resource with no commands carries none")
	}
	if _, has := jsonKeys(t, got)["readable"]; has {
		t.Fatal("the document carries a readable flag; an unreadable resource is omitted instead")
	}
}

func jsonKeys(t *testing.T, doc []byte) map[string]any {
	t.Helper()
	var top struct{ Resources []map[string]any }
	if err := json.Unmarshal(doc, &top); err != nil {
		t.Fatal(err)
	}
	return top.Resources[0]
}

// A resource nobody registered is unreadable: may is nil until RegisterResource
// installs it, so Describe omits it rather than guessing.
func TestDescribeOmitsWhatTheCallerMayNotRead(t *testing.T) {
	t.Parallel()
	out := screens.Describe(t.Context(), []httpx.Resource{resource()})
	if len(out.Resources) != 0 {
		t.Fatalf("an unguarded resource was described: %+v", out)
	}
}
