package rest

// Resource closures share the API transaction, error mapping and row projection.
// Screen adapters call them in-process under the same authorization.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/display"
	"github.com/septagon-oss/platformkit/kit/fault"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/internal/jsonbody"
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

// resource projects a Spec's schema, guards and mounted operation set.
// Withheld operations retain refusing closures for existing consumers.
func (s Spec[T]) resource() httpx.Resource {
	schema := s.Schema()
	res := httpx.Resource{
		Module: s.Module, Entity: s.Entity, Path: s.Path, History: s.historyPort(),
		Read: s.Read, Write: s.Write, OperatorRead: s.OperatorRead, OperatorWrite: s.OperatorWrite,
		ReadBy: s.ReadAuth, WriteBy: s.WriteAuth, Operations: s.Operations,
		Immutable: s.Immutable, Present: s.Present, Schema: schema, RichTextFiles: s.RichTextFiles,
	}
	s.resourceReads(&res)
	s.resourceWrites(&res)
	s.resourceLocales(&res)
	s.restrictResource(&res)
	return res
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
	return m, jsonbody.Decode(b, &m)
}

// decode binds form values exactly and lets encoding/json allocate the entity.
// Custom codecs retain their contract; uncertain floating-point integers refuse.
func decode[T crud.Entity](values map[string]any) (T, error) {
	var e T
	return e, jsonbody.Bind(values, &e)
}

// Form decoding and fault projection stay transport-neutral: strings and maps
// in and out, with HTML composition owned by the caller.

// conditional separates the form's observed revision from entity values.
// Absent means unconditional; malformed revisions are refused before writing.
func conditional(values map[string]any) (string, map[string]any, error) {
	raw, sent := values[entity.RevisionCondition]
	if !sent {
		return "", values, nil
	}
	text, isText := raw.(string)
	if !isText {
		return "", nil, invalid(entity.RevisionCondition, "is not a revision")
	}
	out := make(map[string]any, len(values)-1)
	for name, value := range values {
		if name != entity.RevisionCondition {
			out[name] = value
		}
	}
	if _, err := parseTag(`"` + text + `"`); err != nil {
		return "", out, invalid(entity.RevisionCondition, "is not a whole revision the row can be at")
	}
	return `"` + text + `"`, out, nil
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

// FieldErrorsIn localizes structured rich-text violations into field messages.
// Other faults keep FieldErrors behavior; nil text preserves their source copy.
func FieldErrorsIn(err error, fields []crud.Field, text func(key, fallback string) string) (map[string]string, string) {
	var refused *richTextProblem
	if text == nil || !errors.As(err, &refused) {
		return FieldErrors(err, fields)
	}
	_, known := crud.FieldNamed(fields, refused.field)
	out, lines := map[string]string{}, make([]string, 0, len(refused.issues))
	for _, issue := range refused.issues {
		issue = issue.Localize(text)
		line := fmt.Sprintf("%s %d: %s: %s", text("richtext.line", "line"), issue.Line, issue.Construct, issue.Remedy)
		lines = append(lines, line)
		if known {
			out[refused.field] = strings.TrimPrefix(out[refused.field]+"; "+line, "; ")
		}
	}
	if !known {
		return nil, strings.Join(lines, "; ")
	}
	return out, strings.Join(lines, "; ")
}

// conditionFault names the one field name a generated form could not make sense of: an
// entity column called what an edit form calls the row's condition would be read as a
// condition and never written, which is a silent lie about the person's own submission.
// The name is asked folded, as every other door asks names, because the browser posts
// what the renderer wrote and the decoder would bind either spelling into the column.
func conditionFault(fields []crud.Field) string {
	for _, f := range fields {
		if strings.EqualFold(f.Name, entity.RevisionCondition) {
			return fmt.Sprintf("the entity has a field %q, which is the name an edit form carries the row's revision in",
				entity.RevisionCondition)
		}
	}
	return ""
}

// invalid is a 422 about one field, in the shape FieldErrors reads back.
func invalid(field, why string) error {
	p := problem.New(http.StatusUnprocessableEntity, field+" "+why)
	p.Errors = []string{field + ": " + why}
	p.Violations = []fault.Violation{{Location: field, Code: "invalid", Message: why}}
	return p
}

// FieldErrors maps typed violations and actionable validation prose to known
// fields, returning the overall detail separately. Field names match whole words.
func FieldErrors(err error, fields []crud.Field) (map[string]string, string) {
	p, ok := errors.AsType[*problem.Problem](err)
	if !ok {
		return nil, err.Error()
	}
	if len(p.Violations) > 0 {
		return violationFields(p, fields)
	}
	out := map[string]string{}
	for _, e := range p.Errors {
		if name, message, found := strings.Cut(e, ": "); found {
			if _, known := crud.FieldNamed(fields, name); known {
				if out[name] != "" {
					out[name] += "; "
				}
				out[name] += message
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

// violationFields marks each known field with its violations' messages. The
// whole message is the problem's detail, as for every other refusal. Nothing is
// parsed: a violation's location is the field's wire name or it marks no
// control.
func violationFields(p *problem.Problem, fields []crud.Field) (map[string]string, string) {
	out := map[string]string{}
	for _, v := range p.Violations {
		if _, known := crud.FieldNamed(fields, v.Location); known {
			out[v.Location] = strings.TrimPrefix(out[v.Location]+"; "+v.Message, "; ")
		}
	}
	return out, strings.TrimPrefix(p.Detail, "crud: invalid: ")
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

func (s Spec[T]) resourceReads(res *httpx.Resource) {
	res.Count = func(ctx context.Context) (int64, error) {
		tx, err := transaction(ctx)
		if err != nil {
			return 0, err
		}
		total, err := s.count(ctx, tx)
		return total, Fault(err)
	}
	// The two narrowed counts a declared highlight and trendField ask, in the
	// request's own transaction like the total beside them: the same tenant
	// scope, the same lifecycle predicate, so a sub-count can only ever be a
	// part of the whole one.
	res.CountFiltered = func(ctx context.Context, w crud.Where) (int64, error) {
		if s.Source != nil {
			return 0, Fault(crud.ErrInvalid)
		}
		tx, err := transaction(ctx)
		if err != nil {
			return 0, err
		}
		total, err := crud.CountWhere[T](tx, w)
		return total, Fault(err)
	}
	res.CountDays = func(ctx context.Context, field string, from, to time.Time) ([]crud.DayCount, error) {
		if s.Source != nil {
			return nil, Fault(crud.ErrInvalid)
		}
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		days, err := crud.CountDays[T](tx, field, from, to)
		return days, Fault(err)
	}
	res.List = func(ctx context.Context, q crud.Query) ([]map[string]any, int64, error) {
		tx, err := transaction(ctx)
		if err != nil {
			return nil, 0, err
		}
		items, total, err := s.list(ctx, tx, q)
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
	}
	res.Get = func(ctx context.Context, id uuid.UUID) (map[string]any, error) {
		return answered(ctx, func(tx db.Tx[db.Tenant]) (T, error) { return s.get(ctx, tx, id) })
	}
}

func (s Spec[T]) resourceWrites(res *httpx.Resource) {
	schema := res.Schema
	res.Create = func(ctx context.Context, values map[string]any) (map[string]any, error) {
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
			e, err = s.choiceCreate(e, values)
			if err != nil {
				return e, err
			}
			return s.createRow(ctx, tx, e)
		})
	}
	res.Update = func(ctx context.Context, id uuid.UUID, values map[string]any) (map[string]any, error) {
		return answered(ctx, func(tx db.Tx[db.Tenant]) (T, error) {
			// The form quotes the revision it was drawn from, which is the only
			// answer to "did this person see the row they are editing" that a
			// transaction can check. Absent, the write is what it always was.
			quoted, fields, err := conditional(values)
			if err != nil {
				var none T
				return none, err
			}
			return s.updateChoiceRow(ctx, tx, id, schema.Fields, fields, quoted)
		})
	}
	res.Delete = func(ctx context.Context, id uuid.UUID) error {
		_, err := answered(ctx, func(tx db.Tx[db.Tenant]) (T, error) {
			return s.deleteRow(ctx, tx, id, "")
		})
		return err
	}
}

func (s Spec[T]) resourceLocales(res *httpx.Resource) {
	if s.Translations != nil {
		// The record screen of an entity whose fields a translator owes an answer
		// to shows what is translated in each language this tenant speaks. It is the
		// same read the public page makes — same rows, same withholding of an
		// unreviewed draft, same staleness rule — because a screen that reported a
		// translation no reader would be given is a screen a translator would trust
		// and a translator who trusts it marks the wrong thing reviewed.
		res.Locales = func(ctx context.Context, id uuid.UUID) ([]entity.LocaleState, error) {
			tx, err := transaction(ctx)
			if err != nil {
				return nil, err
			}
			return LocaleStates(ctx, tx, TranslationSourceOf(s), s.Translations, id)
		}
	}
}

func (s Spec[T]) restrictResource(res *httpx.Resource) {
	refused := func(verb string) error {
		return problem.Conflict("a " + s.Entity + " resource offers no " + verb + " route; its Operations name the verbs it has")
	}
	if !s.offers(httpx.CRUDList) {
		// Count goes with the list, not with the read. Its one caller outside this
		// package is the dashboard card (modules/admin/internal/pages.go), and the
		// whole card links to r.Screen — the address of the list page this Spec did
		// not mount. A refused count produces no card, which is the truth; a number
		// on a card that leads nowhere is the lie.
		res.Count = func(context.Context) (int64, error) { return 0, refused("list") }
		res.CountFiltered = func(context.Context, crud.Where) (int64, error) { return 0, refused("list") }
		res.CountDays = func(context.Context, string, time.Time, time.Time) ([]crud.DayCount, error) {
			return nil, refused("list")
		}
		res.List = func(context.Context, crud.Query) ([]map[string]any, int64, error) {
			return nil, 0, refused("list")
		}
	}
	if !s.offers(httpx.CRUDRead) {
		res.Get = func(context.Context, uuid.UUID) (map[string]any, error) { return nil, refused("read") }
	}
	if !s.offers(httpx.CRUDCreate) {
		res.Create = func(context.Context, map[string]any) (map[string]any, error) { return nil, refused("create") }
	}
	if !s.offers(httpx.CRUDUpdate) {
		res.Update = func(context.Context, uuid.UUID, map[string]any) (map[string]any, error) {
			return nil, refused("update")
		}
	}
	if !s.offers(httpx.CRUDDelete) {
		res.Delete = func(context.Context, uuid.UUID) error { return refused("delete") }
	}
}
