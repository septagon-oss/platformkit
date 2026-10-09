package rest

import (
	"fmt"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
)

// display.go is the row-name gate: the mount-time check for `ui:"display"`, the twin of
// presentation.go for the field that names a row. Same reason for the same location: ui/resource is
// what honours the mark, ui/resource imports kit/rest, so a check inside kit/entity would be an import
// cycle, and a Spec is where an entity, a schema and a mount are all in hand at once.
//
// What it prevents is the same silence the other two gates prevent. A mark that means nothing reads
// like a decision: `ui:"display"` on a `time` field would title every record with a timestamp, and two
// marks would make the first one in struct order win by an order no author intended. Both are answers
// the entity's author can fix in the struct, which is why they refuse the mount rather than a screen.

// displayFieldFault names the first field whose `ui:"display"` cannot be honoured, and "" when the
// entity marks at most one field and marks it with a shape a heading can hold.
//
// Zero marks are fine and are today's entity: ui/resource falls back to the first writable
// text-shaped field it declares. A text column is admitted beside a string one because a `text` column
// holds a name as well as a `string` does — the reference app's own users are a `text` table, and
// refusing them would make the mark the only way to be called by one's own name.
func displayFieldFault(fields []crud.Field) string {
	seen := false
	for _, f := range fields {
		if !f.Display {
			continue
		}
		if f.Type != entity.TypeString && f.Type != entity.TypeText {
			return fmt.Sprintf("field %q is marked display and is %s, which is not a string", f.Name, f.Type)
		}
		if seen {
			return fmt.Sprintf("field %q names a second display field; one row has one name", f.Name)
		}
		seen = true
	}
	return ""
}
