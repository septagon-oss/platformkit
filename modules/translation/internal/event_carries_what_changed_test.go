package internal_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/translation"
	"github.com/septagon-oss/platformkit/modules/translation/contracts"
	"github.com/septagon-oss/platformkit/modules/translation/internal"
)

// TestATranslationWriteSaysWhatItReplaced holds the module to the contract every
// audited change carries (kit/events/change.go): translation.updated names what
// the write replaced, in the payload's own member names, so the trail of one
// paragraph's Portuguese is a history and not a list of the rows that survived.
//
// The three cases are the three answers the payload can give: a create has no
// before half, an edit has both, and a removal carries the text it removed beside
// an empty after half and a status of removed — the paragraph a subscriber is
// told about is the one that existed a moment ago, and the payload keeps naming
// it after the row that held it is gone.
func TestATranslationWriteSaysWhatItReplaced(t *testing.T) {
	_, conn := dbtest.Schema(t, translation.Migrations)
	svc := internal.NewService(nil, nil)
	id := uuid.New()

	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		save := func(value string, revision int64) {
			t.Helper()
			if err := svc.Save(ctx, tx, rest.SaveQuery{
				Module: "pages", Entity: "page", Locale: "pt-PT", RecordID: id,
				Values: map[string]string{"title": value}, Expected: map[string]int64{"title": revision},
				Source: map[string]string{"title": "About us."},
			}); err != nil {
				t.Fatalf("saving %q: %v", value, err)
			}
		}

		save("Sobre nós.", 0)
		created := lastEvent(t, ctx, tx)
		if got := changeNames(created.Changes); len(got) != 2 || got["value"] != "→" || got["status"] != "→" {
			t.Errorf("a create's changes = %s, want a value and a status with an after half and no before half", show(created.Changes))
		}
		if created.Value != "Sobre nós." {
			t.Errorf("a create's after half = %q, want the text it stored", created.Value)
		}

		save("Olá a todos.", 1)
		edited := lastEvent(t, ctx, tx)
		moved := halves(edited.Changes, "value")
		if moved[0] != `"Sobre nós."` || moved[1] != `"Olá a todos."` {
			t.Errorf("an edit's value halves = [%s %s], want the text it replaced and the text it became (%s)",
				moved[0], moved[1], show(edited.Changes))
		}

		if err := svc.Untranslate(ctx, tx, rest.ReviewQuery{
			Module: "pages", Entity: "page", Locale: "pt-PT", RecordID: id, Fields: []string{"title"},
		}); err != nil {
			t.Fatalf("untranslating: %v", err)
		}
		removed := lastEvent(t, ctx, tx)
		gone := halves(removed.Changes, "value")
		if gone[0] != `"Olá a todos."` || gone[1] != `""` {
			t.Errorf("a removal's value halves = [%s %s], want the text as it stood and the empty string beside a removed status (%s)",
				gone[0], gone[1], show(removed.Changes))
		}
		if gone[0] == "" || gone[0] == `""` {
			t.Errorf("a removal's event carries no text at all, so a subscriber cannot tell which paragraph vanished")
		}
		if status := halves(removed.Changes, "status"); status[1] != `"removed"` {
			t.Errorf("a removal's status half = %s, want removed", status[1])
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// lastEvent is the translation.updated the transaction published last.
func lastEvent(t *testing.T, ctx context.Context, tx db.Tx[db.Tenant]) contracts.Updated {
	events := publishedPayloads(ctx, tx)
	if len(events) == 0 {
		t.Fatal("the transaction published no translation.updated")
	}
	return events[len(events)-1]
}

// halves is one member's pair of halves, decoded to the json text the trail
// stores, with an absent half as the empty string. Reading the raw bytes rather
// than a Go value is the point: the trail stores payloads verbatim.
func halves(changes []events.Change, name string) [2]string {
	var out [2]string
	for _, c := range changes {
		if c.Field != name {
			continue
		}
		out[0], out[1] = string(c.Before), string(c.After)
	}
	return out
}

func changeNames(changes []events.Change) map[string]string {
	out := map[string]string{}
	for _, c := range changes {
		half := "→"
		if len(c.Before) > 0 && len(c.After) > 0 {
			half = "→←"
		} else if len(c.Before) > 0 {
			half = "←"
		}
		out[c.Field] = half
	}
	return out
}

func show(changes []events.Change) string {
	raw, err := json.Marshal(changes)
	if err != nil {
		return strings.Repeat("?", len(changes))
	}
	return string(raw)
}
