package view_test

import (
	"encoding/json"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// TestAPageWhoseCallerNamedNoHomeWritesNoRootCrumb reads the same row as a record, a
// list and a form from a caller that named no breadcrumb root, and asks that no page
// opens its crumb trail with an empty label: State.Home "" is documented as no root
// crumb at all, so a trail begins with the collection rather than with a crumb that
// names nothing. Every kind is asked twice, once with the root named, because a case
// that only ever saw no root could pass by drawing no crumbs at all.
func TestAPageWhoseCallerNamedNoHomeWritesNoRootCrumb(t *testing.T) {
	t.Parallel()

	row := map[string]any{"id": "t1", "title": "Write", "status": "open",
		"dueAt": "2026-07-01T14:12:00Z", "revision": json.Number("3")}

	for _, kind := range []view.Kind{view.KindRecord, view.KindList, view.KindForm} {
		homeless := crumbsOf(t, kind, "", row)
		named := crumbsOf(t, kind, "Workspace", row)

		if len(named) == 0 {
			t.Fatalf("a %s page built with a root named draws no crumbs at all, so nothing here would be compared", kind)
		}
		if len(homeless) != len(named)-1 {
			t.Errorf("a %s page with no root named draws %d crumbs, want one fewer than the %d it draws with one", kind, len(homeless), len(named))
		}
		for i, crumb := range homeless {
			if crumb.Label == "" {
				t.Errorf("a %s page with no root named writes crumb %d with no label, want no root crumb rather than an empty one", kind, i)
			}
		}
		if len(homeless) > 0 && homeless[0].Label != "tasks" {
			t.Errorf("a %s page with no root named opens its trail with %q, want the collection the page is about", kind, homeless[0].Label)
		}
		if named[0].Label != "Workspace" {
			t.Errorf("a %s page with a root named opens its trail with %q, want Workspace, which is the label the caller gave", kind, named[0].Label)
		}
	}
}

// crumbsOf is the crumb trail one page kind shows over one row when the caller names
// State.Home. A form page is asked as a create, the one form state with no row title
// to show, so all three kinds answer for the same two labels.
func crumbsOf(t *testing.T, kind view.Kind, home string, row map[string]any) []view.Crumb {
	t.Helper()

	in := input(kind, []map[string]any{row}, func(s *view.State) {
		s.Home = home
		if kind == view.KindForm {
			s.Form = view.FormState{Create: true, Title: "New task"}
		}
	})
	return build(t, in).Breadcrumbs
}
