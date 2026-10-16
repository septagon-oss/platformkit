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
// screen renders, or a name that cannot be honoured by the column it sits on,
// and "" when every presentation the entity names is inside the vocabulary
// kit/entity owns. It is widgetFault for the read axis, at the same site, for the
// same reason: a declaration nobody honours is worse than none, because it reads
// like a decision.
func presentationFault(fields []crud.Field) string {
	for _, f := range fields {
		if !entity.ValidPresentation(f.Present) {
			return fmt.Sprintf("field %q names presentation %q, which no screen renders", f.Name, f.Present)
		}
		if bad := pictureFault(f); bad != "" {
			return bad
		}
	}
	return ""
}

// pictureFault refuses the three ways a picture name could disagree with the
// column it sits on: a value that is not a file id, a second reading of the same
// column (`format`, `money`), and a `reference` pointing the id at a resource.
//
// A reference is refused rather than checked because a file id is not a resource
// a screen may point at: `file` registers no resource of its own, so the boot
// gate would refuse the composition for naming it and every installation that
// mounted such a hint would fail to start. Saying it here names the declaration
// that is wrong instead of refusing the whole composition for it.
func pictureFault(f crud.Field) string {
	if !entity.PicturesAre(f.Present) {
		return ""
	}
	list := f.Type == entity.TypeList && f.Elem == entity.TypeUUID
	switch {
	case f.Present == "images" && list:
	case f.Present != "images" && f.Type == entity.TypeUUID:
	default:
		want, shape := "a file id", string(f.Type)
		if f.Present == "images" {
			want = "a list of file ids"
			if f.Type == entity.TypeList {
				shape = "a list of " + string(f.Elem)
			}
		}
		return fmt.Sprintf("field %q is presented %s and is %s, which is not %s", f.Name, f.Present, shape, want)
	}
	if f.Presentation.Format != "" {
		return fmt.Sprintf("field %q is presented %s and is formatted %q; a picture has one reading", f.Name, f.Present, f.Presentation.Format)
	}
	if f.Presentation.Money != nil {
		return fmt.Sprintf("field %q is presented %s and declares money; a picture has one reading", f.Name, f.Present)
	}
	if f.Presentation.Reference != nil {
		return fmt.Sprintf("field %q is presented %s and names a reference; a file id is not a resource a screen may point at", f.Name, f.Present)
	}
	return ""
}
