// Package display is how a field's value reaches a person: the text a control
// carries, the word a screen shows, and the name a field is called by.
//
// It is the one place these are decided, so a list cell, a description list
// and a select's option label agree about what a boolean or an enum looks
// like. It knows the schema and nothing else: no database, no router and no
// markup library, so a screen renderer that must not link the storage adapter
// can still show a value the way the generated screens do. kit/rest keeps
// delegates under the same names for the callers that already read them there.
package display

import (
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/septagon-oss/platformkit/kit/entity"
)

// Text is a value as a form control and a link read it: the raw one. Everything
// arrives as encoding/json made it, so a number is a float64 and a list is a
// []any.
//
// It is not Display. A select's value attribute has to be the enum's own
// spelling and a checkbox's has to be "true"; what a person reads is Display's
// business, and confusing the two is how a form posts back "Yes".
func Text(v any) string {
	switch typed := v.(type) {
	case string:
		return typed
	case bool:
		return strconv.FormatBool(typed)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			parts = append(parts, Text(item))
		}
		return strings.Join(parts, ", ")
	default:
		return ""
	}
}

// Display is a field's value as a screen shows it, and it is the only one: a
// cell, a description list and a select's option label all come through here,
// so a status is "In progress" in all three rather than "in_progress" in two of
// them. An instant is in the form a person reads, a boolean is Yes or No, and
// nothing at all is a dash, because a blank cell reads as a bug rather than as
// an empty field.
func Display(f entity.Field, v any) string {
	switch {
	case f.Type == entity.TypeBool:
		if b, ok := v.(bool); ok && b {
			return "Yes"
		}
		return "No"
	case f.Type == entity.TypeTime:
		if out := moment(v); out != "" {
			return out
		}
	case len(f.Enum) > 0:
		if out := Text(v); out != "" {
			return Humanize(out)
		}
	default:
		if out := Text(v); out != "" {
			return out
		}
	}
	return "—"
}

// moment is how a screen writes an instant. The wire form is RFC 3339 with
// microseconds, which is right for a machine and unreadable in a table cell.
func moment(v any) string {
	raw := Text(v)
	at, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return raw
	}
	return at.UTC().Format("2006-01-02 15:04")
}

// Humanize turns a JSON name or an enum value into something a person reads:
// "slaDeadline" becomes "Sla deadline" and "in_progress" becomes "In progress".
// It is not a dictionary and does not try to be one; a field that wants a
// better word is a field that should say so, which is what Field.Doc is for.
func Humanize(name string) string {
	var b strings.Builder
	for i, r := range name {
		switch {
		case i == 0:
			b.WriteRune(unicode.ToUpper(r))
		case unicode.IsUpper(r):
			b.WriteByte(' ')
			b.WriteRune(unicode.ToLower(r))
		case r == '_' || r == '-':
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// FieldLabel is what a control, a column header and a description term call a
// field. It is Humanize today, in one place, so that the entity gaining a way
// to name its own fields is one line here rather than three at three call
// sites. Field.Doc is not that name: the entities in this repository write a
// sentence there — "Short summary of the task" — which is a description and
// belongs under the control, not on it. See Field.Doc.
func FieldLabel(f entity.Field) string { return Humanize(f.Name) }

// FieldHelp is the note under a control: the field's own Doc, when it has one.
func FieldHelp(f entity.Field) string { return f.Doc }
