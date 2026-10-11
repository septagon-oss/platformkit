package seed

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
)

// A record's identity is the key its owner stores. Two declarations whose keys
// differ only in the spelling that owner folds away are one record named twice,
// and one row cannot meet two sets of fields: the run refuses at the second
// file line and writes nothing, rather than writing whichever came last.
func TestTwoSpellingsOfOneIdentityRefuseAtTheSecondDeclaration(t *testing.T) {
	fold := func(value string) string {
		return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(value)), " ", "-")
	}
	writer := &fakeWriter{
		resource: Resource{
			Alias: "pages", Module: "content", Entity: "content", NaturalKey: "slug",
			WriteGrant: "content.content.write", CanonicalKey: fold,
		},
		rows: map[string]Snapshot{},
	}
	service, err := New(Deps{
		Files: fstest.MapFS{"seed/starter/pages.yaml": {Data: []byte(
			"apiVersion: platformkit.seed/v1\nresource: pages\nrecords:\n" +
				"  - key: About The Team\n    fields: {title: About the team}\n" +
				"  - key: about-the-team\n    fields: {title: About us instead}\n")}},
		Root: "seed", Clock: seedAt, Writers: []Writer{writer}, Authorize: &fakeGrant{},
	})
	if err != nil {
		t.Fatal(err)
	}
	seedTenant(t, false, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := service.Apply(ctx, tx, Selection{})
		if err == nil {
			t.Error("two spellings of one record were accepted; want a refusal naming both declarations")
			return nil
		}
		for _, want := range []string{"about-the-team", "pages.yaml:6:5", "pages.yaml:4:5"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("refusal %q names no %s", err, want)
			}
		}
		if len(writer.writes) != 0 || len(writer.rows) != 0 {
			t.Errorf("a refused run wrote %v to %d rows", writer.writes, len(writer.rows))
		}
		return nil
	})
}
