package rest

import (
	"fmt"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
)

// displayFieldFault validates display hints at mount, where the Spec has both
// entity fields and the presentation contract. The UI consumer imports rest, so
// validating the hint in entity would create an import cycle.

// displayFieldFault returns the first invalid display field, or an empty string
// when there is at most one display hint and its field is string or text. With no
// hint, the UI keeps its existing fallback.
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
