package audittest

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/audit/contracts"
)

func filterTrail(t *testing.T, f Fixture) {
	bob := uuid.New()
	for _, ev := range []events.Event{
		event("task.task.created", ada, noon.Add(-2*time.Hour)),
		event("task.task.created", bob, noon),
		event("user.invited", ada, noon.Add(2*time.Hour)),
	} {
		if err := f.Service.Record(f.Ctx, f.Tx, ev); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	// Since is inclusive and Until exclusive, so the row at noon
	// belongs to exactly one of the two and the halves do not overlap.
	for _, c := range []struct {
		what string
		q    contracts.Query
		want int64
	}{
		{"by name", contracts.Query{Name: "task.task.created"}, 2},
		{"by actor", contracts.Query{Actor: ada}, 2},
		{"since noon", contracts.Query{Since: noon}, 2},
		{"until noon", contracts.Query{Until: noon}, 1},
		{"the hour around noon", contracts.Query{Since: noon, Until: noon.Add(time.Hour)}, 1},
	} {
		rows, total, err := f.Service.List(f.Ctx, f.Tx, c.q)
		if err != nil {
			t.Fatalf("List %s: %v", c.what, err)
		}
		if total != c.want {
			t.Errorf("List %s = %d rows, want %d", c.what, total, c.want)
		}
		if int64(len(rows)) != total {
			t.Errorf("List %s returned %d rows for a total of %d", c.what, len(rows), total)
		}
	}
	// Newest first, because a trail is read from the end.
	rows, _, err := f.Service.List(f.Ctx, f.Tx, contracts.Query{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 3 || rows[0].Name != "user.invited" {
		t.Errorf("the trail starts with %v, want the newest row first", rows)
	}
	// And a page is a page.
	if rows, _, err = f.Service.List(f.Ctx, f.Tx, contracts.Query{Limit: 1, Offset: 1}); err != nil ||
		len(rows) != 1 || rows[0].Name != "task.task.created" {
		t.Errorf("the second row of the trail = %v, %v", rows, err)
	}
}

func filterRecordTrail(t *testing.T, f Fixture) {
	// The three shapes a payload names its row with, and one that
	// names a different row. A generated write carries the row itself;
	// a command carries its own argument, under a name of its own.
	row := uuid.New()
	for _, payload := range []string{
		`{"id":"` + row.String() + `","title":"Buy milk"}`,
		`{"taskId":"` + row.String() + `","to":"somebody"}`,
		`{"changes":[{"record":{"id":"` + row.String() + `"}}]}`,
		`{"id":"` + uuid.NewString() + `"}`,
	} {
		ev := event("task.task.changed", ada, noon)
		ev.Payload = json.RawMessage(payload)
		if err := f.Service.Record(f.Ctx, f.Tx, ev); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	rows, total, err := f.Service.List(f.Ctx, f.Tx, contracts.Query{Record: row})
	if err != nil {
		t.Fatalf("List by record: %v", err)
	}
	if total != 3 || len(rows) != 3 {
		t.Errorf("the trail of one row = %d rows (total %d), want the three that name it", len(rows), total)
	}
	// And it composes with the page, which is what a screen reading a
	// long trail a page at a time depends on.
	if rows, total, err = f.Service.List(f.Ctx, f.Tx, contracts.Query{Record: row, Limit: 2}); err != nil ||
		len(rows) != 2 || total != 3 {
		t.Errorf("a page of one row's trail = %d rows of %d, %v", len(rows), total, err)
	}
}

func unknownAuditRow(t *testing.T, f Fixture) {
	if _, err := f.Service.Get(f.Ctx, f.Tx, uuid.New()); !errors.Is(err, crud.ErrNotFound) {
		t.Errorf("Get of a row nobody has = %v, want ErrNotFound", err)
	}
}
