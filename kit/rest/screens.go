package rest

// screens.go registers the entity itself alongside its routes, so that the
// screens of stage E4 are derived from the same Spec the API is.
//
// The five closures are the five routes without the HTTP: same transaction,
// same errors, same events, same read-only fields. A screen calls them in
// process rather than calling its own API over a socket, which would be a
// second request, a second transaction and a second authorization of a caller
// the first one already recognised.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
)

// answered is the shape every screen closure has: take the request's
// transaction, do one thing, and answer with the one error mapping and the one
// projection.
//
// Spec and Singleton both put themselves onto httpx.Resource and both had a
// copy of this, byte for byte, under two different names. Two copies of an
// error mapping is how two doors come to disagree about what a 409 means.
func answered[T crud.Entity](ctx context.Context, run func(db.Tx[db.Tenant]) (T, error)) (map[string]any, error) {
	tx, err := transaction(ctx)
	if err != nil {
		return nil, err
	}
	e, err := run(tx)
	if err != nil {
		return nil, Fault(err)
	}
	return row(e)
}

// resource is this Spec as httpx.Resource, for Mount to register.
func (s Spec[T]) resource() httpx.Resource {
	schema := s.Schema()
	each := func(ctx context.Context, run func(db.Tx[db.Tenant]) (T, error)) (map[string]any, error) {
		return answered(ctx, run)
	}
	return httpx.Resource{
		Module: s.Module, Entity: s.Entity, Path: s.Path,
		Read: s.Read, Write: s.Write, OperatorWrite: s.OperatorWrite,
		Immutable: s.Immutable, Schema: schema,

		List: func(ctx context.Context, q crud.Query) ([]map[string]any, int64, error) {
			tx, err := transaction(ctx)
			if err != nil {
				return nil, 0, err
			}
			items, total, err := crud.List[T](tx, q)
			if err != nil {
				return nil, 0, Fault(err)
			}
			rows := make([]map[string]any, 0, len(items))
			for _, item := range items {
				r, err := row(item)
				if err != nil {
					return nil, 0, err
				}
				rows = append(rows, r)
			}
			return rows, total, nil
		},
		Get: func(ctx context.Context, id uuid.UUID) (map[string]any, error) {
			return each(ctx, func(tx db.Tx[db.Tenant]) (T, error) { return crud.Get[T](tx, id) })
		},
		Create: func(ctx context.Context, values map[string]any) (map[string]any, error) {
			return each(ctx, func(tx db.Tx[db.Tenant]) (T, error) {
				var e T
				// The same refusal the JSON create gives, at the door a page
				// uses. See refuseImmutable.
				for _, name := range s.Immutable {
					if _, named := values[name]; named {
						return e, immutableRefusal(name)
					}
				}
				e, err := decode[T](values)
				if err != nil {
					return e, err
				}
				crud.Reset(e) // the four fields the server owns, whatever a form sent
				if err := crud.Create(ctx, tx, e); err != nil {
					return e, err
				}
				return e, s.emit(ctx, tx, Created, e, s.AfterCreate)
			})
		},
		Update: func(ctx context.Context, id uuid.UUID, values map[string]any) (map[string]any, error) {
			return each(ctx, func(tx db.Tx[db.Tenant]) (T, error) {
				e, err := crud.Get[T](tx, id)
				if err != nil {
					return e, err
				}
				// merge is the PATCH route's own: read-only and Immutable
				// fields are refused here exactly as they are over HTTP.
				columns, err := merge(e, schema.Fields, s.Immutable, values)
				if err != nil {
					return e, err
				}
				if err := crud.Update(ctx, tx, e, append(columns, "updated_at")...); err != nil {
					return e, err
				}
				return e, s.emit(ctx, tx, Updated, e, nil)
			})
		},
		Delete: func(ctx context.Context, id uuid.UUID) error {
			_, err := each(ctx, func(tx db.Tx[db.Tenant]) (T, error) {
				e, err := crud.Get[T](tx, id)
				if err != nil {
					return e, err
				}
				if err := crud.Delete[T](tx, id, s.SoftDelete); err != nil {
					return e, err
				}
				return e, s.emit(ctx, tx, Deleted, e, s.AfterDelete)
			})
			return err
		},
	}
}

// row is the entity as a screen reads it. It is the entity's own JSON, so a
// column shows what the API would have sent and json:"-" hides a field from
// both at once.
func row(e any) (map[string]any, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	return m, json.Unmarshal(b, &m)
}

// decode builds an entity from a form's values, through the same decoder the
// request body goes through, so "3" is an int in both.
//
// T is a pointer type, so unmarshalling into &e is unmarshalling into a
// **Task, and encoding/json allocates the Task. That is why this needs no
// reflection of its own.
func decode[T crud.Entity](values map[string]any) (T, error) {
	var e T
	b, err := json.Marshal(values)
	if err != nil {
		return e, err
	}
	return e, json.Unmarshal(b, &e)
}

// The rest of this file is what a screen needs to turn a Resource into HTML and
// a submitted form back into a write. It lives here rather than in the one
// module that renders pages today because none of it is about the shell: it is
// the schema read one more way, and a second HTML consumer — a public theme, a
// customer's own admin — would otherwise write it again and disagree about what
// a boolean looks like or which control a refusal belongs to.
//
// Nothing here imports an HTML library. These are strings and maps in, strings
// and maps out; the components are the caller's business.

// Values reads a submitted form into the shape a Resource's write takes, typed
// by the schema rather than by guesswork: a number field arrives as a number, a
// checkbox that was not ticked arrives as false rather than as missing, and a
// blank optional field is left out so a nullable column stays null instead of
// becoming the zero time.
//
// refuse names the fields that must not appear at all — the Immutable ones, on
// a create, which the form does not render. A create is the one door where a
// command's field is otherwise writable, so a value arriving for one did not
// come from the form this function serves, and it is refused with a field error
// rather than dropped: dropping it would store something other than what was
// sent and say nothing. The JSON create keeps its documented behaviour, which
// is that Immutable is about a patch; this is the form's own promise.
func Values(body []byte, fields []crud.Field, refuse []string) (map[string]any, error) {
	form, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, problem.New(http.StatusUnprocessableEntity, "this form could not be read")
	}
	out := map[string]any{}
	for _, f := range fields {
		if f.ReadOnly {
			continue
		}
		raw, sent := form[f.Name]
		if slices.Contains(refuse, f.Name) {
			if sent {
				return nil, invalid(f.Name, "belongs to a route of its own, not to this form")
			}
			continue
		}
		if f.Type == crud.TypeBool {
			// An unticked checkbox sends nothing at all, which is the one case
			// where absence is a value.
			out[f.Name] = sent && raw[0] != "" && raw[0] != "false"
			continue
		}
		if !sent {
			continue
		}
		text := strings.TrimSpace(raw[0])
		if text == "" {
			continue
		}
		switch f.Type {
		case crud.TypeInt:
			n, err := strconv.ParseInt(text, 10, 64)
			if err != nil {
				return nil, invalid(f.Name, "is not a whole number")
			}
			out[f.Name] = n
		case crud.TypeFloat:
			n, err := strconv.ParseFloat(text, 64)
			if err != nil {
				return nil, invalid(f.Name, "is not a number")
			}
			out[f.Name] = n
		case crud.TypeTime:
			// A datetime-local control sends "2026-09-02T14:30" and the API
			// speaks RFC 3339, so the zone the browser did not send is UTC.
			if len(text) == 16 {
				text += ":00"
			}
			if !strings.HasSuffix(text, "Z") && !strings.Contains(text[10:], "+") {
				text += "Z"
			}
			out[f.Name] = text
		case crud.TypeList:
			parts := strings.Split(text, ",")
			list := make([]string, 0, len(parts))
			for _, p := range parts {
				if p = strings.TrimSpace(p); p != "" {
					list = append(list, p)
				}
			}
			out[f.Name] = list
		default:
			out[f.Name] = text
		}
	}
	return out, nil
}

// Writable drops the fields a route of its own owns. The update route refuses
// them with a 422 naming the field, which is right for an API and wrong for a
// form that rendered them read-only and then posted them back — a browser posts
// a read-only control's value, and the person changed nothing.
func Writable(values map[string]any, immutable []string) map[string]any {
	out := make(map[string]any, len(values))
	for name, v := range values {
		if !slices.Contains(immutable, name) {
			out[name] = v
		}
	}
	return out
}

// invalid is a 422 about one field, in the shape FieldErrors reads back.
func invalid(field, why string) error {
	p := problem.New(http.StatusUnprocessableEntity, field+" "+why)
	p.Errors = []string{field + ": " + why}
	return p
}

// FieldErrors reads a problem back into the fields it is about, so a form marks
// the control rather than only shouting above it, and returns the whole message
// as well. kit/problem's Errors carry "field: message"; a Detail that names a
// field is matched too, because kit/crud's own messages are prose that names it.
//
// The Detail match is on the field's name as a word of its own. It used to be a
// substring, so a message about "subtitle" marked "title" — the wrong control,
// with the right message, which is the confusing half of both.
func FieldErrors(err error, fields []crud.Field) (map[string]string, string) {
	p, ok := err.(*problem.Problem)
	if !ok {
		return nil, err.Error()
	}
	out := map[string]string{}
	for _, e := range p.Errors {
		if name, message, found := strings.Cut(e, ": "); found {
			if _, known := crud.FieldNamed(fields, name); known {
				out[name] = message
			}
		}
	}
	detail := strings.TrimPrefix(p.Detail, "crud: invalid: ")
	for _, f := range fields {
		if _, taken := out[f.Name]; !taken && mentions(detail, f.Name) {
			out[f.Name] = detail
		}
	}
	return out, detail
}

// mentions reports whether detail names field as a word of its own.
func mentions(detail, field string) bool {
	word := func(b byte) bool {
		return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
	}
	for i := 0; i+len(field) <= len(detail); i++ {
		if detail[i:i+len(field)] != field {
			continue
		}
		if (i == 0 || !word(detail[i-1])) && (i+len(field) == len(detail) || !word(detail[i+len(field)])) {
			return true
		}
	}
	return false
}

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
func Display(f crud.Field, v any) string {
	switch {
	case f.Type == crud.TypeBool:
		if b, ok := v.(bool); ok && b {
			return "Yes"
		}
		return "No"
	case f.Type == crud.TypeTime:
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
func FieldLabel(f crud.Field) string { return Humanize(f.Name) }

// FieldHelp is the note under a control: the field's own Doc, when it has one.
func FieldHelp(f crud.Field) string { return f.Doc }
