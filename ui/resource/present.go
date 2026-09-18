package resource

// present.go composes the read axis: a field says what its value *is* with
// `ui:"present:…"` and this package decides what that looks like, through the
// components that own the composition. The vocabulary is kit/entity's, the same
// way the write axis's is, and for the same documented reason — a name no
// renderer honours has to refuse to mount rather than print the value and look
// like it worked.
//
// One composition exists, `person`. That is deliberate and it is not a start of a
// series: kit/crud's ADR-0012 rule is that a part earns its place with a consumer
// outside the package that wrote it, and the users list is the consumer in sight.
// Every other field on every screen in this repository renders exactly as it did
// before this file existed, because returning nil from the cell slot is how the
// table is told "the plain value" — see components.TableSlots.Cell.
//
// What is *not* here: the detail screen. components.DetailItem.Value is a string,
// so a description list cannot carry a disc, and changing it is a change to an
// exported component contract that moves the exported vocabulary — a decision for
// whoever needs a person's face on a record page, not a side effect of this one.

import (
	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/display"
	"github.com/septagon-oss/platformkit/ui/components"
)

// compose is the read axis answering one cell. It returns nil when the field names
// nothing, which is how the table is told "draw the value yourself" — see
// components.TableSlots.Cell — and every field on every screen that was written
// before this file existed takes that path.
//
// `primary` is whether this column is the row's way into the record. It changes the
// answer rather than being ignored by it: the identity column is already a link, and
// a cell that grew a disc while losing its link would have quietly taken the mouse
// away from the person using the screen. So the disc *is* the way in, named after the
// person, and the name beside it stays plain text — said once, followed once.
func compose(f entity.Field, row components.TableRow, primary bool, at string) g.Node {
	if f.Present == "" {
		return nil
	}
	text := display.Text(row.Cells[f.Name])
	switch f.Present {
	case "person":
		// An unclaimed handle is a field nobody filled in, and every other empty
		// field on this screen says so with a dash. A generic face there would be
		// the screen claiming it knows whose picture is missing.
		if text == "" || text == "—" {
			return nil
		}
		person := components.AvatarProps{Name: text, Label: text, Size: "sm"}
		if primary {
			person.Href = at + "/" + row.ID
		}
		return components.Avatar(person)
	}
	// A mounted Spec cannot reach here: kit/rest refuses a presentation outside the
	// vocabulary at mount. It returns the plain value rather than panicking because
	// List is also called directly, by tests and by the design export, and a
	// renderer that panics there is one nobody can extend.
	// TestEveryPresentationNameInTheVocabularyComposesSomething is what stops an
	// entry arriving in kit/entity without a case in this switch.
	return nil
}

// fieldFor finds a column's field by its schema name. The table is built from
// schema fields and hands a cell back by key, so this is the lookup that ties the
// two ends of one render together.
func fieldFor(fields []entity.Field, name string) (entity.Field, bool) {
	for _, f := range fields {
		if f.Name == name {
			return f, true
		}
	}
	return entity.Field{}, false
}
