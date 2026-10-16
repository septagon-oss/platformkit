package resource_test

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/ui/resource"
)

// timeAttrs reads the datetime and title of every <time> element in rendered markup.
var timeAttrs = regexp.MustCompile(`<time datetime="([^"]*)"[^>]*title="([^"]*)"`)

// TestAnInstantsTitleIsTheSameUTCInstantItsDatetimeCarries: a value stored with an
// offset is titled as the UTC instant in ISO form, naming the same moment the machine
// attribute carries, on both the list and the record — never the offset wall time
// and never a spelled zone word that no catalogue owns.
func TestAnInstantsTitleIsTheSameUTCInstantItsDatetimeCarries(t *testing.T) {
	t.Parallel()
	row := map[string]any{"id": "1", "title": "Buy milk", "created": "2026-07-01T19:42:00+05:30"}
	for name, out := range map[string]string{
		"list":   render(t, resource.List(timed(), opts, []map[string]any{row}, 1, 1, "", false).Body),
		"record": render(t, resource.Detail(timed(), opts, row, true).Body),
	} {
		m := timeAttrs.FindAllStringSubmatch(out, -1)
		if len(m) != 1 {
			t.Fatalf("%s: %d time elements with datetime and title, want 1:\n%s", name, len(m), out)
		}
		if m[0][2] != "2026-07-01T14:12:00Z" {
			t.Errorf("%s: title %q, want the UTC instant 2026-07-01T14:12:00Z", name, m[0][2])
		}
		at, err1 := time.Parse(time.RFC3339, m[0][1])
		titled, err2 := time.Parse(time.RFC3339, m[0][2])
		if err1 != nil || err2 != nil || !at.Equal(titled) {
			t.Errorf("%s: datetime %q and title %q do not name one instant", name, m[0][1], m[0][2])
		}
		if strings.Contains(m[0][2], "UTC") || strings.Contains(m[0][2], "+05:30") {
			t.Errorf("%s: title %q spells a zone or keeps the stored offset", name, m[0][2])
		}
	}
}
