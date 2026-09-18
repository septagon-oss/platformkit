package rest

import (
	"fmt"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
)

// presentation.go is the read axis's mount gate, the mirror of the widget gate in
// widget.go. Separate file, same reason: a Spec is where an entity, a schema and a
// mount are all in hand at once, and the failure both gates prevent is silence — a
// declaration nobody honours reads like a decision.
//
// The vocabulary itself is kit/entity's, and the gate cannot live there: ui/resource
// is what honours a `present:` name, ui/resource imports kit/rest, so a check inside
// kit/entity would be an import cycle. The only place a name can be checked against
// the words that exist is the package that owns both ends.

// presentationFault names the first field whose `ui:"present:…"` is a name no
// screen renders, and "" when every presentation the entity names is inside the
// vocabulary kit/entity owns. It is widgetFault for the read axis, at the same
// site, for the same reason: a declaration nobody honours is worse than none,
// because it reads like a decision.
func presentationFault(fields []crud.Field) string {
	for _, f := range fields {
		if !entity.ValidPresentation(f.Present) {
			return fmt.Sprintf("field %q names presentation %q, which no screen renders", f.Name, f.Present)
		}
	}
	return ""
}
