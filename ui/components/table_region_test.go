package components

// table_region_test.go pins the keyboard contract of the table's scroll wrapper.
//
// The wrapper is `overflow: auto`, so a table wider than its box hides its own
// right-hand columns. A pointer reaches them by dragging or wheeling; a keyboard
// reaches them only if the scrolling box can take focus, and a box that can take
// focus without saying what it is adds an anonymous landmark to the page. Both
// halves are pinned here: a named table becomes a reachable region, and an
// unnamed one is refused the affordance rather than given an invented name.

import (
	"strings"
	"testing"
)

func tableProps(label string) TableProps {
	return TableProps{
		Label:   label,
		Columns: []TableColumn{{Key: "name", Label: "Name"}, {Key: "role", Label: "Role"}},
		Rows: []TableRow{
			{ID: "u1", Cells: map[string]any{"name": "Ada", "role": "admin"}},
		},
	}
}

func TestNamedTableIsAKeyboardReachableRegion(t *testing.T) {
	t.Parallel()

	out := renderNode(t, Table(tableProps("Tasks")))

	for _, want := range []string{`role="region"`, `tabindex="0"`, `aria-label="Tasks"`} {
		if !strings.Contains(out, want) {
			t.Errorf("named table is missing %s\n%s", want, out)
		}
	}
	// The name is only worth anything on a box that can actually scroll. The
	// renderer cannot assert what CSS does, but it can pin the one utility this
	// contract depends on: if the wrapper's geometry ever changes, the test says
	// so rather than letting the region claim a reachability the class list no
	// longer provides.
	wrapper := strings.SplitN(out, "<table", 1)[0]
	if !strings.Contains(wrapper, "overflow-auto") {
		t.Errorf("the named region is no longer a scroll box: %s", wrapper)
	}
}

func TestUnnamedTableIsNotMadeAnAnonymousRegion(t *testing.T) {
	t.Parallel()

	for name, label := range map[string]string{
		"absent":     "",
		"whitespace": "   ",
	} {
		out := renderNode(t, Table(tableProps(label)))
		if strings.Contains(out, "tabindex") {
			t.Errorf("%s label: the wrapper takes focus without a name: %s", name, out)
		}
		if strings.Contains(out, `role="region"`) {
			t.Errorf("%s label: the wrapper becomes a landmark nobody named: %s", name, out)
		}
	}
}

func TestOnlyTheScrollWrapperCarriesTheRegion(t *testing.T) {
	t.Parallel()

	out := renderNode(t, Table(tableProps("Tasks")))
	if n := strings.Count(out, "tabindex"); n != 1 {
		t.Errorf("expected one focusable node, the scroll wrapper; found %d in %s", n, out)
	}
	wrapper, rest, _ := strings.Cut(out, "<table")
	if !strings.Contains(wrapper, `tabindex="0"`) {
		t.Errorf("the focus affordance is not on the scroll wrapper: %s", out)
	}
	if strings.Contains(rest, "tabindex") || strings.Contains(rest, `role="region"`) {
		t.Errorf("the region leaked inside the table, where a row already carries the focus: %s", out)
	}
}

func TestACallersOwnAttributesAreHonouredNotDuplicated(t *testing.T) {
	t.Parallel()

	// ComponentProps.Attrs is a free-form map emitted verbatim, so a caller can
	// declare tabindex or role itself. Two attributes of one name are invalid HTML,
	// and the browser keeps the first, so the only honest outcomes are "theirs" or
	// "ours" - never both, and never a tag claiming an attribute that does not apply.
	owned := tableProps("Tasks")
	owned.Attrs = map[string]string{"tabindex": "-1", "role": "group", "aria-label": "Named by its caller"}
	out := renderNode(t, Table(owned))

	for _, name := range []string{"tabindex", "role", "aria-label"} {
		if n := strings.Count(out, name+"="); n != 1 {
			t.Errorf("expected exactly one %s attribute, found %d: %s", name, n, out)
		}
	}
	if !strings.Contains(out, `tabindex="-1"`) {
		t.Errorf("the caller's own tabindex was overwritten: %s", out)
	}
	if !strings.Contains(out, `role="group"`) {
		t.Errorf("the caller named the element something else and the renderer argued: %s", out)
	}
	if !strings.Contains(out, `aria-label="Named by its caller"`) {
		t.Errorf("the caller supplied the name and the renderer's was emitted anyway: %s", out)
	}
}

// The table skeleton stands in for a table that has not arrived, and is hidden
// from assistive technology while it does: naming a placeholder would announce
// a region whose contents are shimmer.
func TestTableSkeletonStaysHiddenAndUnfocusable(t *testing.T) {
	t.Parallel()

	out := renderNode(t, TableSkeleton(TableSkeletonProps{Columns: 2, Rows: 3}))
	if !strings.Contains(out, `aria-hidden="true"`) {
		t.Errorf("the table skeleton is no longer hidden from assistive technology: %s", out)
	}
	if strings.Contains(out, "tabindex") {
		t.Errorf("the table skeleton takes keyboard focus while it is only a placeholder: %s", out)
	}
}
