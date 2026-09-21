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
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity/display"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
)

// answered applies the shared transaction, error mapping and row projection
// for collection and singleton screen operations.
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
	return httpx.Resource{
		Module: s.Module, Entity: s.Entity, Path: s.Path,
		Read: s.Read, Write: s.Write, OperatorRead: s.OperatorRead, OperatorWrite: s.OperatorWrite,
		Immutable: s.Immutable, Schema: schema,

		Count: func(ctx context.Context) (int64, error) {
			tx, err := transaction(ctx)
			if err != nil {
				return 0, err
			}
			total, err := crud.Count[T](tx)
			return total, Fault(err)
		},
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
			return answered(ctx, func(tx db.Tx[db.Tenant]) (T, error) { return crud.Get[T](tx, id) })
		},
		Create: func(ctx context.Context, values map[string]any) (map[string]any, error) {
			return answered(ctx, func(tx db.Tx[db.Tenant]) (T, error) {
				var e T
				// The same refusal the JSON create gives, at the door a page
				// uses, folded the same way: decode hands these values to the
				// same decoder, so a key that folds onto a reserved name binds
				// into the entity rather than being ignored. See foldedName.
				if name := foldedName(values, s.Immutable); name != "" {
					return e, immutableRefusal(name)
				}
				e, err := decode[T](values)
				if err != nil {
					return e, err
				}
				return s.createRow(ctx, tx, e)
			})
		},
		Update: func(ctx context.Context, id uuid.UUID, values map[string]any) (map[string]any, error) {
			return answered(ctx, func(tx db.Tx[db.Tenant]) (T, error) {
				return s.updateRow(ctx, tx, id, schema.Fields, values)
			})
		},
		Delete: func(ctx context.Context, id uuid.UUID) error {
			_, err := answered(ctx, func(tx db.Tx[db.Tenant]) (T, error) {
				return s.deleteRow(ctx, tx, id)
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
// becoming the zero time. UpdateValues also handles clearing existing values.
//
// refuse names the fields that must not appear at all — the Immutable ones, on
// a create, which the form does not render. A create is the one door where a
// command's field is otherwise writable, so a value arriving for one did not
// come from the form this function serves, and it is refused with a field error
// rather than dropped: dropping it would store something other than what was
// sent and say nothing. The name is matched folded, as at every other door.
func Values(body []byte, fields []crud.Field, refuse []string) (map[string]any, error) {
	return formValues(body, fields, refuse, false)
}

// UpdateValues reads an edit form, retaining submitted blank text, empty lists
// and null optional instants while omitting absent and command-owned fields.
func UpdateValues(body []byte, fields []crud.Field, immutable []string) (map[string]any, error) {
	values, err := formValues(body, fields, nil, true)
	return Writable(values, immutable), err
}

func formValues(body []byte, fields []crud.Field, refuse []string, update bool) (map[string]any, error) {
	form, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, problem.New(http.StatusUnprocessableEntity, "this form could not be read")
	}
	// Asked of every posted key before any field is read, so a key that folds
	// onto a refused name is refused and not merely left out of the lookup.
	if name := foldedName(form, refuse); name != "" {
		return nil, invalid(name, "belongs to a route of its own, not to this form")
	}
	out := map[string]any{}
	for _, f := range fields {
		if f.ReadOnly {
			continue
		}
		if slices.Contains(refuse, f.Name) {
			continue
		}
		raw, sent := form[f.Name]
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
			if update {
				switch f.Type {
				case crud.TypeString, crud.TypeText:
					out[f.Name] = ""
				case crud.TypeList:
					out[f.Name] = []string{}
				case crud.TypeTime:
					if !f.Required {
						out[f.Name] = nil
					}
				}
			}
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
			// Browser-local values use UTC; explicit offsets retain their instant.
			at, err := time.Parse(time.RFC3339Nano, text)
			if err != nil {
				at, err = time.Parse("2006-01-02T15:04:05", text)
			}
			if err != nil {
				at, err = time.Parse("2006-01-02T15:04", text)
			}
			if err != nil {
				return nil, invalid(f.Name, "is not a time")
			}
			out[f.Name] = at.Format(time.RFC3339Nano)
		case crud.TypeList:
			parts := strings.Split(text, ",")
			list := make([]string, 0, len(parts))
			for _, p := range parts {
				if p = strings.TrimSpace(p); p != "" {
					list = append(list, p)
				}
			}
			out[f.Name] = list
			if f.Elem == crud.TypeInt || f.Elem == crud.TypeFloat || f.Elem == crud.TypeBool {
				typed := make([]any, 0, len(list))
				for _, value := range list {
					item, err := coerce(crud.Field{Name: f.Name, Type: f.Elem}, value)
					number, numeric := item.(float64)
					if err != nil || (numeric && (math.IsNaN(number) || math.IsInf(number, 0))) {
						return nil, invalid(f.Name, "contains an invalid "+string(f.Elem))
					}
					typed = append(typed, item)
				}
				out[f.Name] = typed
			}
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
// as well. kit/problem's Errors carry "field: message"; a validation Detail that
// names a field is matched too, because kit/crud's validation messages are prose.
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
	// Only validation prose can infer a field. A conflict can mention a value
	// or title without identifying the control that needs changing.
	if p.Status == http.StatusUnprocessableEntity {
		for _, f := range fields {
			if _, taken := out[f.Name]; !taken && mentions(detail, f.Name) {
				out[f.Name] = detail
			}
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

// The value words — Text, Display, Humanize, FieldLabel and FieldHelp — are
// kit/entity/display's, so a screen renderer that must not link the storage
// adapter shows a value the way the generated screens do. These delegates keep
// the names the existing callers read here.

// Text is display.Text: a value as a form control and a link read it.
func Text(v any) string { return display.Text(v) }

// Display is display.Display: a field's value as a screen shows it.
func Display(f crud.Field, v any) string { return display.Display(f, v) }

// Humanize is display.Humanize: a JSON name or an enum value as a person reads it.
func Humanize(name string) string { return display.Humanize(name) }

// FieldLabel is display.FieldLabel: what a control and a column header call a field.
func FieldLabel(f crud.Field) string { return display.FieldLabel(f) }

// FieldHelp is display.FieldHelp: the note under a control, the field's own Doc.
func FieldHelp(f crud.Field) string { return display.FieldHelp(f) }
