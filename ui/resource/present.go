package resource

// Resolved media kinds select existing components. Delivery templates come
// from the adapter; picture bytes and access checks remain at their declared door.

import (
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/display"
	"github.com/septagon-oss/platformkit/ui/components"
)

// compose renders a supplied media kind, returning nil for a plain value.
// primary preserves record navigation; name labels a picture used as its link.
// at is the list's record address — empty for a resource that mounted no record
// route, which leaves every primary cell drawing its value unlinked.
func compose(f entity.Field, row components.TableRow, primary bool, at string, o Options, name, missing string) g.Node {
	if f.Type == entity.TypeSketch {
		return sketchReading(row.Cells[f.Name], name, "thumbnail", o.Words(entity.Schema{Fields: []entity.Field{f}}).text)
	}
	// A declared choice answers through its resolved policy, which owns the
	// appearance; a plain value leaves nil for the table to draw itself.
	if f.Choices != nil {
		node := (Resource{Schema: entity.Schema{Fields: []entity.Field{f}}}).choiceRead(o, f, row.Cells[f.Name])
		if href := recordLink(at, row.ID); primary && href != "" {
			return h.A(h.Href(href), node)
		}
		return node
	}
	if f.Present == "" {
		return nil
	}
	text := display.Text(row.Cells[f.Name])
	// An empty field is a field nobody filled in, and every other empty field on
	// this screen says so with a dash. A placeholder picture there would be the
	// screen claiming it knows what is missing.
	blank := text == ""
	switch f.Present {
	case "person":
		if blank {
			return nil
		}
		person := components.AvatarProps{Name: text, Label: text, Size: "sm"}
		if primary {
			person.Href = recordLink(at, row.ID)
		}
		return components.Avatar(person)
	case "picture", "image", "images", "avatar":
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
				return g.Text(missing)
			}
		}
		if blank || id == "" || src == "" {
			return nil
		}
		href := ""
		if primary {
			href = recordLink(at, row.ID)
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

// recordLink is one row's record page: the list's record address and the row's
// id, or empty for a resource with no record route. Every primary cell below
// draws the plain value rather than a link into the 404 of a place nothing serves.
func recordLink(at, id string) string {
	if at == "" {
		return ""
	}
	return at + "/" + id
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
