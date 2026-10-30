package rest

import (
	"fmt"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
)

// presentationFault validates presentation hints where the entity schema and
// resource mount are both available. The UI consumer imports rest, so the check
// cannot live in entity without creating an import cycle.

// presentationFault returns the first invalid presentation declaration, or an
// empty string when every field's presentation, choices and image hints are valid.
func presentationFault(fields []crud.Field) string {
	for _, f := range fields {
		if bad := f.SketchFault(); bad != "" {
			return bad
		}
		if err := entity.ValidateChoices(f); err != nil {
			return fmt.Sprintf("field %q: %s", f.Name, err)
		}
		if !entity.ValidPresentation(f.Present) {
			return fmt.Sprintf("field %q names presentation %q, which no screen renders", f.Name, f.Present)
		}
		if bad := pictureFault(f); bad != "" {
			return bad
		}
	}
	return ""
}

// pictureFault checks the scalar, read-only declaration and competing hints for
// stored or composed images. A composed reference is never a file or resource id.
func pictureFault(f crud.Field) string {
	if f.Type == entity.TypeList && f.Elem == entity.TypePicture {
		return fmt.Sprintf("field %q is a list of composed pictures, which has no presentation", f.Name)
	}
	if f.Type == entity.TypePicture && f.Present != "image" {
		return fmt.Sprintf("picture field %q requires present:image", f.Name)
	}
	if !entity.PicturesAre(f.Present) {
		return ""
	}
	list := f.Type == entity.TypeList && f.Elem == entity.TypeUUID
	switch {
	case f.Present == "image" && f.Type == entity.TypePicture:
		if !f.ReadOnly {
			return fmt.Sprintf("picture field %q must declare readOnly", f.Name)
		}
		if f.Widget != "" {
			return fmt.Sprintf("picture field %q cannot declare a widget", f.Name)
		}
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
