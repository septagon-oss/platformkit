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
		// Moment answers the raw text for a value that is no instant, which is what
		// Display has always shown for one; only an absent value is a dash.
		if _, out, _ := Moment(v); out != "" {
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

// Instant is a time field's value as the moment it is: what a screen puts in a
// `<time datetime>` attribute, and what the reader's own engine turns into their
// own wall time. The wire form is RFC 3339 with microseconds, which a table cell
// cannot be asked to read; the instant is the part that was missing, not the
// words. A value that is not an instant answers ok false — the raw string stays
// plain text, because an invented date would be a fiction a person cannot tell
// from the real one.
func Instant(v any) (at time.Time, ok bool) {
	raw := Text(v)
	if raw == "" {
		return at, false
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return at, false
	}
	return parsed, true
}

// Moment is an instant as a screen writes it, and as it did before the reader's
// own clock was asked: UTC, "2026-07-01 14:12". It stays the answer every
// reading shares — the cell, the description list and everything kit/rest
// delegates — so that the zone a person reads is chosen by the browser that
// knows it (see ui/assets/js/times.js) rather than baked into a string one
// server wrote. ok is false for a value that is no instant, and the raw text is
// what comes back: a person is shown what is stored, not a dash pretending the
// value was absent.
func Moment(v any) (at time.Time, text string, ok bool) {
	at, ok = Instant(v)
	if !ok {
		return at, Text(v), false
	}
	return at, at.UTC().Format("2006-01-02 15:04"), true
}

// massNouns are the English words that are already a set: an entity named
// "Content" is not pluralised into "Contents" any more than one named "Settings"
// becomes "Settingss". The list is closed on purpose — it is the shape of the
// defect the screens showed, not an English dictionary — and it is the same
// eight words the phone client refuses, so the two screens answer one question
// the same way. A noun a client needs added is a translation of that client's
// own vocabulary, not a change to this shared rule.
var massNouns = map[string]struct{}{
	"content": {}, "settings": {}, "news": {}, "media": {},
	"data": {}, "staff": {}, "feedback": {}, "information": {},
}

// Plural is how a screen writes the name of a set: "Task" becomes "Tasks" and
// "Content" stays "Content", because the catalogue already wrote it as a set. A
// word that ends in "s" is kept as it is written, case and all, since the rule
// that adds an "s" to it is the rule that produced "Settingss". An empty name
// answers empty: there is no noun to inflect, and "s" would be a word nobody
// asked about. Only the comparison is trimmed — what comes back is what went in.
func Plural(one string) string {
	if strings.TrimSpace(one) == "" {
		return one
	}
	key := strings.ToLower(strings.TrimSpace(one))
	if _, mass := massNouns[key]; mass || strings.HasSuffix(key, "s") {
		return one
	}
	return one + "s"
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
func FieldLabel(f entity.Field) string {
	if f.Presentation.Label != "" {
		return f.Presentation.Label
	}
	return Humanize(f.Name)
}

// FieldHelp is the note under a control: the line the author wrote for this
// field when they wrote one, and the field's own Doc otherwise.
func FieldHelp(f entity.Field) string {
	if f.Presentation.Help != "" {
		return f.Presentation.Help
	}
	return f.Doc
}
