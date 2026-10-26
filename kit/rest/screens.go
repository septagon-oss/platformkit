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
		Module: s.Module, Entity: s.Entity, Name: s.Name, Path: s.Path, History: s.historyPort(),
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
		return "", nil, invalid(entity.RevisionCondition, "invalid.rest.revision.shape", "is not a revision")
	}
	out := make(map[string]any, len(values)-1)
	for name, value := range values {
		if name != entity.RevisionCondition {
			out[name] = value
		}
	}
	if _, err := parseTag(`"` + text + `"`); err != nil {
		return "", out, invalid(entity.RevisionCondition, "invalid.rest.revision.number", "is not a whole revision the row can be at")
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

// invalid is a 422 about one field, in the shape FieldErrors reads back: the whole
// sentence in the Detail and the same sentence again under the field's name, so the
// screen marks that control rather than only shouting above it. One sentence in both
// places is what lets a reader be shown one translated sentence rather than half a
// page in each of two.
//
// It is a fault.Reason — verdict, text, key and the arguments the text wants —
// because a form's refusal reaches a person, and that person's language is not this
// package's to decide. ui/resource is the catalogue that answers the key, being the
// only one a generated form is ever drawn from.
func invalid(field, key, text string, args ...any) error {
	reason := &fault.Reason{Kind: crud.ErrInvalid, Key: key, Text: text, Args: args}
	return refusedProblem(reason, field)
}

// refusedForm is the one refusal of this package's with no field to mark: the body
// that arrived was not a form at all, so its sentence stands on its own.
func refusedForm(key, text string, args ...any) error {
	return refusedProblem(&fault.Reason{Kind: crud.ErrInvalid, Key: key, Text: text, Args: args}, "")
}

// refusedProblem is a keyed refusal in the problem shape the form reads back: the
// Detail is the writer's sentence, Errors repeats it under the field it is about,
// and Refusal is what carries it — so the page shows the same sentence in the
// reader's language and the problem document and the log keep this one.
func refusedProblem(reason *fault.Reason, field string) error {
	p := problem.New(http.StatusUnprocessableEntity, reason.Error())
	if field != "" {
		p.Errors = []string{field + ": " + reason.Error()}
		p.Violations = []fault.Violation{{Location: field, Code: "invalid", Message: reason.Error()}}
	}
	p.Refusal = reason
	return p
}

// richTextErrors is FieldErrors for the one refusal this package can take apart:
// a rich-text refusal carries its structured issues beside the problem, because
// the English sentences are the contract the JSON API answers with and a form
// redrawn in another language must not print them at a reader who asked in
// theirs. text is that language's formatter — the same one that names the form's
// own labels — and each issue's key is in its package's catalogue.
//
// It reports whether the refusal was one of these; every other refusal is prose
// this package cannot take apart, and comes back from FieldErrors as it stands.
func richTextErrors(err error, fields []crud.Field, text func(key, fallback string) string) (map[string]string, string, bool) {
	var refused *richTextProblem
	if text == nil || !errors.As(err, &refused) {
		return nil, "", false
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
		return nil, strings.Join(lines, "; "), true
	}
	return out, strings.Join(lines, "; "), true
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

// FieldErrors reads a problem back into the fields it is about, so a form marks
// the control rather than only shouting above it, and returns the whole message
// as well. kit/problem's Errors carry "field: message"; a validation Detail that
// names a field is matched too, because kit/crud's validation messages are prose.
//
// The Detail match is on the field's name as a word of its own. It used to be a
// substring, so a message about "subtitle" marked "title" — the wrong control,
// with the right message, which is the confusing half of both.
func FieldErrors(err error, fields []crud.Field) (map[string]string, string) {
	return FieldErrorsWithWords(err, fields, nil)
}

// Words is a request's language, as far as a form's refusal needs it: one
// sentence asked of the catalogue the reader is being answered from. kit/locale's
// Locale satisfies it; this package names the port rather than importing that one
// because reading a refusal costs a lookup and not a negotiation.
type Words interface {
	Text(key, fallback string, args ...any) string
}

// FieldErrorsIn is FieldErrors for a screen that does not speak English. A
// rich-text refusal carries its structured issues beside the problem, because
// the English sentences are the contract the JSON API answers with and a form
// redrawn in another language must not print them at a reader who asked in
// theirs. text is that language's formatter — the same one that names the form's
// own labels — and each issue's key is in its package's catalogue.
//
// Every other refusal is prose this package cannot take apart, so it comes back
// from FieldErrors as it stands. A caller that also wants a module's own refusal
// sentence read in the reader's language asks FieldErrorsWithWords, which is this
// function's rich-text answer and the whole refusal in one pass.
func FieldErrorsIn(err error, fields []crud.Field, text func(key, fallback string) string) (map[string]string, string) {
	if text == nil {
		return FieldErrors(err, fields)
	}
	marked, detail, richtext := richTextErrors(err, fields, text)
	if !richtext {
		return FieldErrors(err, fields)
	}
	return marked, detail
}

// FieldErrorsWithWords is FieldErrors told in the language words answers in: the
// same controls marked, the same sentence above them, and — where the module that
// refused the write ships its copy in this composition — that sentence read in the
// reader's own language rather than the one the Go source is written in. Which
// control is at fault is worked out from the source sentence first, because the
// source sentence is the one that names the field: a Portuguese reason says
// "título", and a match on the English word would lose the marker from the control
// the moment the reason was translated. A refusal with no key, a composition with no
// catalogue, and a catalogue with no entry for the key are all answered the source
// sentence, which is what FieldErrors answers. It is a second name beside
// FieldErrorsIn rather than a wider parameter on it: taking this package's Words port
// instead of a two-argument function changes that signature, and a frozen release line
// keeps the one a caller compiled against — docs/COMPATIBILITY.md.
func FieldErrorsWithWords(err error, fields []crud.Field, words Words) (map[string]string, string) {
	if words != nil {
		if marked, detail, richtext := richTextErrors(err, fields,
			func(key, fallback string) string { return words.Text(key, fallback) }); richtext {
			return marked, detail
		}
	}
	p, ok := errors.AsType[*problem.Problem](err)
	if !ok {
		return nil, err.Error()
	}
	if len(p.Violations) > 0 {
		marked, whole := violationFields(p, fields)
		return told(err, p, words, marked, whole)
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
	return told(err, p, words, out, detail)
}

// told answers a refusal whose fields are already marked in the reader's language:
// each message that is still one of the writer's own sentences is replaced by that
// sentence as the catalogue words it, and the whole message above the controls with
// it. The sentences it knows are the rules a validator reported and the one sentence
// the problem carries; a refusal carrying neither, a caller with no catalogue, a
// catalogue with no entry for the key, and a message a driver or this package wrote
// are all left as they were marked.
func told(err error, p *problem.Problem, words Words, out map[string]string, detail string) (map[string]string, string) {
	if words == nil {
		return out, detail
	}
	lines := map[string]string{}
	if said, source := spoken(p, words); said != "" {
		lines[source] = said
	}
	var keyed *fault.Keyed
	if errors.As(err, &keyed) {
		for _, one := range keyed.Rules {
			if source := one.Reason.Error(); source != "" {
				lines[source] = words.Text(one.Reason.Key, one.Reason.Text, one.Reason.Args...)
			}
		}
	}
	for name, message := range out {
		if line, named := lines[message]; named && line != message {
			out[name] = line
		}
	}
	if line, named := lines[detail]; named && line != detail {
		detail = line
	}
	return out, detail
}

// spoken is the writer's own sentence for this refusal, in the reader's language,
// and the source sentence it stands for ("" for "" when there is no such sentence
// to ask about). Three things have to hold: the writer carried one, the caller can
// ask a language, and the Detail actually printed is that sentence — either bare or
// behind the marker kit/crud puts in front of a refusal. A Detail this package or a
// driver wrote instead keeps its own words: copy keyed to a sentence nobody is being
// shown would be a sentence swapped in under the reader's eyes.
func spoken(p *problem.Problem, words Words) (said, source string) {
	if p.Refusal == nil || words == nil {
		return "", ""
	}
	source = p.Refusal.Error()
	if p.Detail != source && !strings.HasSuffix(p.Detail, ": "+source) {
		return "", source
	}
	// The fallback is the writer's sentence with its holes still in it, not the one
	// already filled that the Detail carries: the arguments belong to the template,
	// and handing a formatter a filled sentence plus the arguments it no longer
	// holds is answered with `%!(EXTRA …)` — the reader's own language with the
	// writer's formatting left on top. Asked with the template, a catalogue that
	// holds no entry answers exactly the sentence already on the page, which is the
	// answer that means "nothing translated here" and leaves the Detail alone.
	if translated := words.Text(p.Refusal.Key, p.Refusal.Text, p.Refusal.Args...); translated != source {
		return translated, source
	}
	return "", source
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
