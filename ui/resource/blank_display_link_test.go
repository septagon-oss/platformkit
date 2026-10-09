package resource_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/resource"
)

func TestABlankDisplayFieldKeepsTheListLinkNamed(t *testing.T) {
	t.Parallel()
	const id = "6f1c2d3e-0000-4000-8000-000000000002"
	row := map[string]any{"id": id, "email": "ada@acme.example", "name": ""}
	title := resource.Detail(member(), opts, row, false).Title
	if title != "ada@acme.example" {
		t.Fatalf("record title = %q, want the next available field", title)
	}

	markup := render(t, resource.List(member(), opts, []map[string]any{row}, 1, 1, "", false).Body)
	at := strings.Index(markup, `href="/app/member/members/`+id+`"`)
	if at < 0 {
		t.Fatalf("list has no link to the record:\n%s", markup)
	}
	open, close := strings.Index(markup[at:], ">"), strings.Index(markup[at:], "</a>")
	if open < 0 || close <= open {
		t.Fatalf("record link has no readable contents:\n%s", markup[at:])
	}
	if got := markup[at+open+1 : at+close]; !strings.Contains(got, title) {
		t.Errorf("list link says %q while the record title says %q", got, title)
	}
}
