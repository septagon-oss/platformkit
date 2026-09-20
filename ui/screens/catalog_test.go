package screens_test

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
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
// copied by hand into platformkit-mobile/testdata, which is a weaker link than
// this comment would once have implied: nothing compares the two, so a change
// here does not fail there — the shell's tests keep passing against the copy it
// still holds, and the difference is met in production by a build that cannot be
// redeployed as quickly as this repository can be. `catalogVersion` is the handle
// for closing that: compare this file to the one at the shell's pinned foundation
// version, and let a future build refuse a shape it was not written for.
// Regenerate with
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
	// A singleton: one row per tenant, at the path itself. A shell that could
	// not tell it from a collection would draw a list with a New button on it
	// and no route to serve either.
	settings := resource()
	settings.Entity, settings.Path, settings.Singleton = "setting", "/api/v1/note/settings", true
	settings.Schema.Entity, settings.Schema.Path = "setting", "/api/v1/note/settings"
	settings.Immutable = nil
	catalog := screens.Catalog{Version: screens.CatalogVersion, Resources: []screens.Entry{
		screens.Describe1(notes, true),
		screens.Describe1(tags, false),
		screens.Describe1(settings, true),
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
	if len(back.Resources) != 3 || back.Resources[0].Fields[0].Name != "id" || !back.Resources[0].Fields[0].ReadOnly {
		t.Fatalf("the catalog does not round-trip: %+v", back)
	}
	// The golden is copied verbatim into the native shell's testdata, so the
	// version is not decoration here: it is the one thing in the document that
	// tells a future build whether it may render what it is being handed.
	if back.Version != screens.CatalogVersion {
		t.Fatalf("the golden carries catalog version %d, not %d", back.Version, screens.CatalogVersion)
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
	if back.Resources[0].Singleton || !back.Resources[2].Singleton {
		t.Fatal("singleton did not survive the trip")
	}
	// Both renderings of the same record are in the document now, so the one
	// thing the addition cannot be is a second opinion: every `schema` is what
	// entity.JSONSchema makes of the `fields` beside it, entity or command.
	for _, e := range back.Resources {
		assertSchemaIsTheProjection(t, e.Entity, e.JSONSchema, e.Fields)
		for _, c := range e.Commands {
			assertSchemaIsTheProjection(t, e.Entity+" command "+c.Verb, c.Schema, c.Fields)
		}
	}
	if _, has := jsonKeys(t, got)["readable"]; has {
		t.Fatal("the document carries a readable flag; an unreadable resource is omitted instead")
	}
}

// assertSchemaIsTheProjection compares a schema the document carries with the
// one entity.JSONSchema makes of the fields beside it, as the bytes each side
// encodes to. Schema and fields describe one record, and a shell that reads one
// and a shell that reads the other have to be told the same record.
func assertSchemaIsTheProjection(t *testing.T, what string, got json.RawMessage, fields []entity.Field) {
	t.Helper()
	if len(fields) == 0 {
		if len(got) != 0 {
			t.Errorf("%s carries a schema built out of no fields: %s", what, got)
		}
		return
	}
	doc, err := entity.JSONSchema(fields)
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	// The document is indented, and a raw member keeps the whitespace it was
	// read with, so the comparison is of the two compact forms. Both sides are
	// bytes encoding/json wrote, which is what makes their key order one order
	// and not one per run.
	var compact bytes.Buffer
	if err := json.Compact(&compact, got); err != nil {
		t.Fatalf("%s: the schema in the document is not JSON: %v", what, err)
	}
	if !bytes.Equal(compact.Bytes(), want) {
		t.Errorf("%s: the served schema is not entity.JSONSchema of the fields beside it\n got %s\nwant %s", what, compact.String(), want)
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

// TestTheRouteStampsTheCatalogVersion. Describe is the function mounted at
// /api/v1/admin/resources; the golden above is written from a literal, so
// nothing else would notice a served document arriving with no version at all —
// and a zero there reads as "very old", which is the opposite of the truth. The
// resource below is deliberately unreadable, because the stamp is not a function
// of what the caller may see.
func TestTheRouteStampsTheCatalogVersion(t *testing.T) {
	t.Parallel()
	body, err := json.Marshal(screens.Describe(t.Context(), []httpx.Resource{resource()}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"catalogVersion":1`) {
		t.Errorf("the served document does not carry the catalog version: %s", body)
	}
}
