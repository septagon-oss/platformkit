package present_test

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
)

func TestTitleFallsFromCandidatesToIDToEmpty(t *testing.T) {
	t.Parallel()
	row := map[string]any{"a": "  ", "b": "Bee"}
	for _, c := range []struct {
		names    []string
		id, want string
	}{
		{[]string{"a", "b"}, "i", "Bee"},
		{[]string{"a"}, "i", "i"},
		{nil, " ", "—"},
	} {
		if got := present.Title(c.names, row, c.id, "—"); got != c.want {
			t.Errorf("Title(%v, %q) = %q, want %q", c.names, c.id, got, c.want)
		}
	}
}

func TestAgoTruncatesTowardZeroInSignedUnits(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		at   time.Duration
		unit string
		n    int
	}{
		{-30 * time.Second, "second", -30}, {-90 * time.Second, "minute", -1},
		{3 * time.Hour, "hour", 3}, {-49 * time.Hour, "day", -2},
	} {
		if unit, n := present.Ago(now.Add(c.at), now); unit != c.unit || n != c.n {
			t.Errorf("Ago(%v) = %d %s, want %d %s", c.at, n, unit, c.n, c.unit)
		}
	}
}

func TestResolveLeavesTheSharedRegistrationUntouchedUnderConcurrency(t *testing.T) {
	t.Parallel()
	in := present.Input{
		Schema: entity.Schema{Entity: "note", Fields: []entity.Field{
			{Name: "id", Type: entity.TypeUUID, ReadOnly: true},
			{Name: "title", Type: entity.TypeString},
			{Name: "status", Type: entity.TypeString, Enum: []string{"a", "b"}},
		}},
		Hints: entity.EntryHints{Sortable: []string{"title"}},
	}
	before := in.Schema.Fields[2].Enum[0]
	want := present.Resolve(in)
	var wg sync.WaitGroup
	results := make([]present.Resolved, 8)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lang := in
			lang.Language = "pt-PT"
			lang.Text = func(_, fallback string) string { return fallback + "!" }
			results[i] = present.Resolve(lang)
			_ = present.Resolve(in)
		}()
	}
	wg.Wait()
	for _, got := range results {
		if got.Fields[2].Reading.Missing != "—!" || got.Language != "pt-PT" {
			t.Fatalf("language leaked: %+v", got.Fields[2].Reading)
		}
	}
	if again := present.Resolve(in); !reflect.DeepEqual(again, want) || in.Schema.Fields[2].Enum[0] != before {
		t.Fatal("a resolved language changed the registration")
	}
}
