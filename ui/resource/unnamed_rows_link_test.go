package resource_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/resource"
)

// Two rows with a blank name on one list page are two different records, so the
// links into them must not read alike: each takes its own row's fallback.
func TestTwoUnnamedRowsAreLinkedByTwoNames(t *testing.T) {
	t.Parallel()
	rows := []map[string]any{
		{"id": "6f1c2d3e-0000-4000-8000-000000000003", "email": "ada@acme.example", "name": ""},
		{"id": "6f1c2d3e-0000-4000-8000-000000000004", "email": "grace@acme.example"},
	}
	markup := render(t, resource.List(member(), opts, rows, 1, 1, "", false).Body)
	for _, row := range rows {
		id, want := row["id"].(string), row["email"].(string)
		at := strings.Index(markup, `href="/app/member/members/`+id+`"`)
		if at < 0 {
			t.Fatalf("list has no link to %s:\n%s", id, markup)
		}
		open, close := strings.Index(markup[at:], ">"), strings.Index(markup[at:], "</a>")
		if open < 0 || close <= open {
			t.Fatalf("link to %s has no readable contents:\n%s", id, markup[at:])
		}
		if got := markup[at+open+1 : at+close]; !strings.Contains(got, want) {
			t.Errorf("link to %s says %q, want its own row's %q", id, got, want)
		}
	}
}
