package rest

import (
	"fmt"
	"strings"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
)

func entryHintsFault(name string, p entity.EntryHints, fields []crud.Field) string {
	if bad := entryLayoutFault(p); bad != "" {
		return bad
	}
	if bad := entryImageFault(name, p, fields); bad != "" {
		return bad
	}
	if bad := entryNamedFieldsFault(name, p, fields); bad != "" {
		return bad
	}
	if bad := entrySortableFault(name, p, fields); bad != "" {
		return bad
	}
	bounds, bad := declaredBoundsFault(name, p.Highlight, fields)
	if bad != "" {
		return bad
	}
	if bad := emptyWindowFault(bounds); bad != "" {
		return bad
	}
	if p.TrendField != "" {
		f, ok := entity.FieldNamed(fields, p.TrendField)
		if !ok {
			return fmt.Sprintf("trendField %q is not a field of %s", p.TrendField, name)
		}
		if f.Type != entity.TypeTime {
			return fmt.Sprintf("trendField %q is a %s; a day breakdown reads a column of days", p.TrendField, f.Type)
		}
	}
	return entity.DeckFault(p, fields)
}

func entryLayoutFault(p entity.EntryHints) string {
	if !entity.ValidIcon(p.Icon) {
		return fmt.Sprintf("icon %q is not an icon; entity.IconNames admits %s", p.Icon, strings.Join(entity.IconNames(), ", "))
	}
	if p.Order < 0 {
		return fmt.Sprintf("order %d is not a position; 0 means \"no opinion\"", p.Order)
	}
	if p.Group != nil {
		if !lowerIdentifier(p.Group.Key) {
			return fmt.Sprintf("group key %q is not a lower-case identifier", p.Group.Key)
		}
		if p.Group.Label == "" {
			return fmt.Sprintf("group %q declares no label; a nav entry with no words is a nav entry nobody reads", p.Group.Key)
		}
	}
	sections := map[string]bool{}
	for _, sec := range p.Sections {
		if sections[sec.Key] {
			return fmt.Sprintf("section key %q is declared twice; one key names one block", sec.Key)
		}
		sections[sec.Key] = true
		if sec.Label == "" {
			return fmt.Sprintf("section %q declares no label; a block a person cannot name is a block they cannot ask for", sec.Key)
		}
	}
	// A declared-but-empty list is the shape that reads as a decision and changes
	// nothing, so it refuses; an absent one is silence and does not.
	for _, list := range []struct {
		key   string
		field []string
	}{
		{"sortable", p.Sortable}, {"summaryFields", p.SummaryFields},
	} {
		if list.field != nil && len(list.field) == 0 {
			return fmt.Sprintf("%s declares no field; say nothing rather than nothing", list.key)
		}
	}
	if p.Sections != nil && len(p.Sections) == 0 {
		return "sections declares no block; say nothing rather than nothing"
	}
	return ""
}

func entryImageFault(name string, p entity.EntryHints, fields []crud.Field) string {
	for _, named := range []struct {
		key   string
		field string
		row   bool
	}{
		{"coverField", p.CoverField, false}, {"thumbnailField", p.ThumbnailField, true},
	} {
		// A picture a person cannot see is not a picture a screen opens on, and a
		// thumbnail drawn from a field the entry keeps off the list is a declaration
		// nothing honours: `detail` and `hidden` both take the column away from a row.
		if named.field == "" {
			continue
		}
		if bad := namedFieldFault(named.key, named.field, name, fields, false); bad != "" {
			return bad
		}
		f, _ := entity.FieldNamed(fields, named.field)
		if f.Present != "image" {
			return fmt.Sprintf("%s %q is not a picture; present:image names the field that holds one", named.key, named.field)
		}
		if named.row && !f.OnList() {
			return fmt.Sprintf("%s %q names a field kept off the list; a row cannot show what the entry hid from it", named.key, named.field)
		}
	}
	return ""
}

func entryNamedFieldsFault(name string, p entity.EntryHints, fields []crud.Field) string {
	// An empty name is silence, not a hint naming the empty string.
	if p.PrimaryField != "" {
		if bad := namedFieldFault("primaryField", p.PrimaryField, name, fields, false); bad != "" {
			return bad
		}
	}
	for _, named := range []struct{ key, field string }{
		{"previewField", p.PreviewField}, {"statusField", p.StatusField},
	} {
		// previewField and statusField are optional, so an empty name is silence.
		if named.field == "" {
			continue
		}
		if bad := namedFieldFault(named.key, named.field, name, fields, false); bad != "" {
			return bad
		}
	}
	for _, field := range p.SummaryFields {
		if field == p.PrimaryField {
			return fmt.Sprintf("summaryFields repeats primaryField %q; a row says its name once", field)
		}
		if bad := namedFieldFault("summaryFields", field, name, fields, false); bad != "" {
			return bad
		}
	}
	return ""
}

func entrySortableFault(name string, p entity.EntryHints, fields []crud.Field) string {
	for _, field := range p.Sortable {
		f, ok := entity.FieldNamed(fields, field)
		if !ok {
			return fmt.Sprintf("sortable %q is not a field of %s", field, name)
		}
		// The same fact kit/crud refuses at query time, refused where it was
		// written: a list is not something to sort a column on.
		if f.Type == entity.TypeList {
			return fmt.Sprintf("sortable %q is a list, which is not something to sort on", field)
		}
		if f.Type == entity.TypePicture {
			return fmt.Sprintf("sortable %q is a derived picture, which cannot be sorted", field)
		}
		if f.Presentation.Hidden() {
			return fmt.Sprintf("sortable %q names a hidden field; a column nobody sees is not a column to order by", field)
		}
	}
	return ""
}
