package resource

// present.go composes the read axis: a field says what its value *is* with
// `ui:"present:…"` and this package decides what that looks like, through the
// components that own the composition. The vocabulary is kit/entity's, the same
// way the write axis's is, and for the same documented reason — a name no
// renderer honours has to refuse to mount rather than print the value and look
// like it worked.
//
// One composition existed until the pictures arrived, `person`. Every other
// field on every screen in this repository renders exactly as it did before this
// file existed, because returning nil from the cell slot is how the table is told
// "the plain value" — see components.TableSlots.Cell. A picture field returns nil
// too whenever there is no picture to draw: no value, no address for one, or a
// column kept off the row.
//
// A picture is fetched, never embedded: the address comes from Options.Pictures
// and the bytes come from the declared delivery door after its access checks.
// The cell renders no inline document and makes no source access decision.
//
// What is *not* here: the detail screen. components.DetailItem.Value is a string,
// so a description list cannot carry a disc or a photo, and changing it is a
// change to an exported component contract that moves the exported vocabulary — a
// decision for whoever needs a person's face on a record page, not a side effect
// of this one.

import (
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

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
//
// `name` is the row's own name, which is what a picture that is the row's only way
// in is called by. Beside the name it is decorative: a picture read next to the words
// it illustrates is the same information twice, and a list of thirty rows is thirty
// repetitions of it.
func compose(f entity.Field, row components.TableRow, primary bool, at string, o Options, name string, words Words) g.Node {
	if f.Type == entity.TypeSketch {
		return sketchReading(row.Cells[f.Name], words.FieldLabel(f), "thumbnail", words.text)
	}
	if f.Present == "" {
		return nil
	}
	text := display.Text(row.Cells[f.Name])
	// An empty field is a field nobody filled in, and every other empty field on
	// this screen says so with a dash. A placeholder picture there would be the
	// screen claiming it knows what is missing.
	blank := text == "" || text == "—"
	switch f.Present {
	case "person":
		if blank {
			return nil
		}
		person := components.AvatarProps{Name: text, Label: text, Size: "sm"}
		if primary {
			person.Href = at + "/" + row.ID
		}
		return components.Avatar(person)
	case "image", "images", "avatar":
		id := text
		if f.Present == "images" {
			id = firstID(text)
		}
		src := o.pictureSource(id)
		if f.Type == entity.TypePicture {
			src = ""
			if _, err := entity.PictureKey(entity.Picture(id)); err == nil && o.ComposedSource != "" {
				src = strings.ReplaceAll(o.ComposedSource, "{reference}", id)
			}
			if blank || src == "" {
				return g.Text("—")
			}
		}
		if blank || id == "" || src == "" {
			return nil
		}
		href := ""
		if primary {
			href = at + "/" + row.ID
		}
		if f.Present == "avatar" {
			// A person's picture is the same disc `person` draws, with the prop it
			// has always taken: the value differs — a file id rather than a name —
			// and the reading does not.
			return components.Avatar(components.AvatarProps{
				Src: src, Size: "sm", Href: href, AriaLabel: name, Decorative: href == "",
			})
		}
		picture := components.Media(components.MediaProps{
			Src: src, Alt: name, Horizontal: true, Lazy: true, Fit: "cover",
			// A cover with no words beside it is the row's name; one with words
			// beside it says nothing the text next to it does not.
			Decorative: href == "" || name == "",
		})
		if href == "" {
			return picture
		}
		return h.A(h.Href(href), picture)
	}
	// A mounted Spec cannot reach here: kit/rest refuses a presentation outside the
	// vocabulary at mount. It returns the plain value rather than panicking because
	// List is also called directly, by tests and by the design export, and a
	// renderer that panics there is one nobody can extend.
	return nil
}

// pictureSource is the address a cell's picture is fetched from, and "" when this
// installation names no door for one — see Options.PictureSource.
func (o Options) pictureSource(id string) string {
	if o.PictureSource == "" || id == "" {
		return ""
	}
	return strings.ReplaceAll(o.PictureSource, "{id}", id)
}

// firstID is the first id of a `present:images` cell, which is what a row draws: a
// row is one line tall, and the order the owner chose is the order the list of
// values arrives in. display.Text spells that list out with ", " and this is the
// one reading of it a cell needs.
func firstID(text string) string {
	first, _, _ := strings.Cut(text, ", ")
	return strings.TrimSpace(first)
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
