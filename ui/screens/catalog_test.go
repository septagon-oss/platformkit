package screens_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/ui/screens"
)

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
	catalog := screens.Catalog{Resources: []screens.Entry{
		screens.Describe1(resource(), true),
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
