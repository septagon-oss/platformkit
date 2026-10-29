package rest

import (
	"fmt"
	"strings"
	"time"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

// hints.go is the presentation gate: the mount-time refusal of a declaration
// about how a resource reads that could only ever draw the wrong screen. It is
// widgetFault and presentationFault for the reading contract, at the same site
// and for the same reason — a Spec is where an entity, a schema and a mount are
// all in hand at once, and a declaration nobody honours reads like a decision.
//
// The vocabularies themselves are kit/entity's, because ui/ draws them and a
// kernel below the presentation layer cannot import what it must check against.
// The one thing that cannot be checked here is whether a `reference` names a
// module this installation composed: Spec.check runs per Spec and registration
// order is not a fact it can see, so CheckReferences below asks the question at
// boot, where the whole resource list exists.

// entryHintFault names what an entry's own hints get wrong, and "" when they are
// coherent. Every refusal is a mount-time panic, like the rest of check.
func (s Spec[T]) entryHintFault() string {
	if s.Present.Deck != nil && (!s.offers(List)) {
		return "deck requires a list resource"
	}
	if bad := entryHintsFault(s.Entity, s.Present, crud.Fields[T]()); bad != "" {
		return bad
	}
	// A sub-count and a day breakdown are questions the list asks: the totals are
	// refused for a Spec that mounts no list, so declaring either here would be a
	// hint nothing would ever answer — the same question kit/rest/screens.go asks
	// before it fills Count at all.
	if (s.Present.Highlight != nil || s.Present.TrendField != "") && !s.offers(httpx.CRUDList) {
		return "highlight and trendField name questions only a list route answers, and this resource mounts none"
	}
	return ""
}

// bounds is one field's declared window: the two instants it is bounded by, when
// the declaration named an instant, and the anchor word it named otherwise. Kept
// per field because a window is a question about one column.
type bounds struct {
	from, to         time.Time
	fromWord, toWord string
	hasFrom, hasTo   bool
}

// declaredOps is what a declared sub-count may compare with. It is narrower than
// crud.Ops on purpose: the rest of the grammar narrows a page a server builds, and a
// declaration is a module's own sentence about a card, which is the promise worth
// holding at mount. The list is named here rather than read from crud.Ops so the
// refusal says what this gate admits and not what the statement grammar can spell.
var declaredOps = []string{
	string(crud.OpEq), string(crud.OpLt), string(crud.OpLte), string(crud.OpGt), string(crud.OpGte),
}

// declaredBoundsFault checks one declared sub-count: the words, the predicates,
// and each predicate against the field it names. Every refusal here is about a
// declaration that could only ever be answered wrongly — the honest answer to
// "Due today" written against a column that cannot hold a day is nothing at all,
// and the alternative is the total in a card that says "Due today".
func declaredBoundsFault(name string, h *entity.EntryHighlight, fields []crud.Field) (map[string]*bounds, string) {
	if h == nil {
		return nil, ""
	}
	if h.Label == "" {
		return nil, "highlight declares no label; a second number beside the total is a number a person has to be told the name of"
	}
	if len(h.Filter) == 0 {
		return nil, "highlight declares no predicate; a label with no predicate is the total wearing a label"
	}
	window := map[string]*bounds{}
	for _, pred := range h.Filter {
		f, ok := entity.FieldNamed(fields, pred.Field)
		if !ok {
			return nil, fmt.Sprintf("highlight filter names %q, which is not a field of %s", pred.Field, name)
		}
		if f.Type == entity.TypeList {
			return nil, fmt.Sprintf("highlight filter names %q, which is a list; a list holds many values and one of them is not what the count is about", pred.Field)
		}
		if !crud.ValidOp(pred.Op) {
			return nil, fmt.Sprintf("highlight filter op %q is no operator; a declared count asks with %s", pred.Op, strings.Join(declaredOps, ", "))
		}
		switch crud.Op(pred.Op) {
		case crud.OpEq:
		case crud.OpLt, crud.OpLte, crud.OpGt, crud.OpGte:
			if f.Type != entity.TypeTime && f.Type != entity.TypeInt && f.Type != entity.TypeFloat {
				return nil, fmt.Sprintf("highlight filter %s %q %q compares a %s, and only a count, an amount or a moment is asked about with an order", pred.Field, pred.Op, pred.Value, f.Type)
			}
		default:
			return nil, fmt.Sprintf("highlight filter %s %q %q is an operator a declared count does not ask with; it admits %s", pred.Field, pred.Op, pred.Value, strings.Join(declaredOps, ", "))
		}
		if _, err := crud.Declared(f, pred.Value, time.Time{}); err != nil {
			return nil, fmt.Sprintf("highlight filter %s %q %q is refused: %s", pred.Field, pred.Op, pred.Value, err)
		}
		w := window[pred.Field]
		if w == nil {
			w = &bounds{}
			window[pred.Field] = w
		}
		switch crud.Op(pred.Op) {
		case crud.OpGt, crud.OpGte:
			if w.hasFrom {
				return nil, fmt.Sprintf("highlight filter bounds %s from twice; one column has one lower edge in one question", pred.Field)
			}
			w.hasFrom, w.fromWord = true, pred.Value
			w.from, _ = instantOf(f, pred.Value)
		case crud.OpLt, crud.OpLte:
			if w.hasTo {
				return nil, fmt.Sprintf("highlight filter bounds %s from above twice; one column has one upper edge in one question", pred.Field)
			}
			w.hasTo, w.toWord = true, pred.Value
			w.to, _ = instantOf(f, pred.Value)
		}
	}
	return window, ""
}

// instantOf is the declared value as an instant, when the field is a moment and
// the value is an RFC3339 instant. An anchor word answers no instant at mount —
// it means a different one in every response — and is compared by rank instead.
func instantOf(f entity.Field, raw string) (time.Time, bool) {
	if f.Type != entity.TypeTime || entity.ValidAnchor(raw) {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, raw)
	return t, err == nil
}

// anchorRank orders the three clock words without knowing the instant they are
// read against, which is the whole reason a declaration can be checked at mount:
// the day opens, the request arrives somewhere inside it, the next day opens.
var anchorRank = map[string]float64{"today": 0, "now": 0.5, "tomorrow": 1}

// emptyWindowFault refuses the pair of bounds that admit nothing — `dueAt >=
// today AND dueAt < today` is a question with one answer, and a card that counts
// zero is not that answer. Only a pair the mount can order is refused: an instant
// against an anchor depends on the response, and a declaration that is sometimes
// empty is a question, not a mistake.
func emptyWindowFault(window map[string]*bounds) string {
	for field, w := range window {
		if !w.hasFrom || !w.hasTo {
			continue
		}
		from, to := w.from, w.to
		lower, upper := anchorRank[w.fromWord], anchorRank[w.toWord]
		switch {
		case entity.ValidAnchor(w.fromWord) && entity.ValidAnchor(w.toWord):
			if lower < upper {
				continue
			}
		case !entity.ValidAnchor(w.fromWord) && !entity.ValidAnchor(w.toWord) && !from.IsZero() && !to.IsZero():
			if from.Before(to) {
				continue
			}
		default:
			continue
		}
		return fmt.Sprintf("highlight filter on %s admits nothing: %q is not before %q", field, w.fromWord, w.toWord)
	}
	return ""
}

// namedFieldFault refuses a hint that names no field, or names one its author
// also asked to keep off every screen. allowHidden is false for the pointers
// that draw a row: nothing about how a resource reads is served by naming the
// plumbing as its face.
func namedFieldFault(key, field, name string, fields []crud.Field, allowHidden bool) string {
	f, ok := entity.FieldNamed(fields, field)
	if !ok {
		return fmt.Sprintf("%s %q is not a field of %s", key, field, name)
	}
	if !allowHidden && f.Presentation.Hidden() {
		return fmt.Sprintf("%s %q names a hidden field; a row is not named by its plumbing", key, field)
	}
	return ""
}

// fieldHintFault names what a field's hints get wrong, one field per sentence.
// It runs over an entity's fields and over a command's argument, which is why
// sections is a parameter and not read from a Spec: a command's input belongs to
// no entry of its own.
func fieldHintFault(name string, fields []crud.Field, p entity.EntryHints) string {
	for _, f := range fields {
		h := f.Presentation
		if bad := hintWordsFault(f); bad != "" {
			return bad
		}
		if !entity.ValidFormat(h.Format) {
			return fmt.Sprintf("field %q is formatted %q, which is not a format entity.FormatNames admits", f.Name, h.Format)
		}
		if !entity.ValidVisibility(h.Visibility) {
			return fmt.Sprintf("field %q declares visibility %q, which is not a visibility entity.VisibilityNames admits", f.Name, h.Visibility)
		}
		if bad := enumHintFault(f); bad != "" {
			return bad
		}
		if bad := doneValuesFault(f, p); bad != "" {
			return bad
		}
		if bad := moneyFault(name, fields, f); bad != "" {
			return bad
		}
		if h.Reference != nil && !referenceTarget(h.Reference.Resource) {
			return fmt.Sprintf("reference %q is not \"module/entity\"", h.Reference.Resource)
		}
		// `visibility` beside `hide:list` is not a refusal in either direction. The
		// two ways to name one column agree by precedence, which entity.Field.OnList
		// resolves the same way for the gate and the renderer: the explicit
		// visibility is the author's word about the field's reading and wins over the
		// older tag, `shown` reclaims a column `hide:list` took and `detail` gives up
		// one it left. Refusing the pair that disagreed while saying nothing about the
		// pair that agreed (`hidden` beside `hide:list`, where both want the column
		// off) would refuse the wrong half.
		if h.Section != "" && !hasSection(p.Sections, h.Section) {
			return fmt.Sprintf("field %q names section %q, which the entry declares no section as", f.Name, h.Section)
		}
	}
	return ""
}

// hintWordsFault refuses the tag grammar's own limit: it has no escape sequence,
// and inventing one so a label can hold an equals sign is a worse answer than a
// shorter label.
func hintWordsFault(f crud.Field) string {
	for _, m := range []struct {
		key   string
		value map[string]string
	}{
		{"enum label", f.Presentation.EnumLabels},
		{"enum tone", f.Presentation.EnumTones},
	} {
		for key := range m.value {
			if strings.Contains(key, "=") {
				return fmt.Sprintf("field %q declares %s %q; the tag grammar has no escape", f.Name, m.key, key)
			}
		}
	}
	return ""
}

func enumHintFault(f crud.Field) string {
	for label, values := range map[string]map[string]string{
		"tone": f.Presentation.EnumTones, "label": f.Presentation.EnumLabels,
	} {
		if len(values) == 0 {
			continue
		}
		if len(f.Enum) == 0 {
			return fmt.Sprintf("field %q declares enum %s values and has no enum", f.Name, label)
		}
		for value, said := range values {
			if !slicesContain(f.Enum, value) {
				return fmt.Sprintf("field %q declares an enum %s for %q, which its enum does not hold", f.Name, label, value)
			}
			if label == "tone" && !entity.ValidTone(said) {
				return fmt.Sprintf("field %q names tone %q for value %q, which is not a tone entity.ToneNames admits", f.Name, said, value)
			}
		}
	}
	return ""
}

// doneValuesFault refuses the shapes where a list of "finished" values could
// disagree with the field it sits on: no enum to name a value of, a value the
// enum does not hold, the same value written twice, a list declared and emptied,
// and a field that is not the entry's status field.
//
// That last refusal is the stricter of the two readings of "on a status field",
// and it is the one that cannot lie: the state a shell celebrates is the one the
// row's pill shows, and the pill is `statusField`'s. It costs nothing elsewhere —
// a command's argument is checked against the zero EntryHints, so a form field is
// never a record's state either.
func doneValuesFault(f crud.Field, p entity.EntryHints) string {
	done := f.Presentation.DoneValues
	if done == nil {
		return ""
	}
	if len(done) == 0 {
		return fmt.Sprintf("field %q declares no done value; say nothing rather than nothing", f.Name)
	}
	if len(f.Enum) == 0 {
		return fmt.Sprintf("field %q declares done values and has no enum", f.Name)
	}
	if p.StatusField != f.Name {
		return fmt.Sprintf("field %q declares done values and is not this resource's status field", f.Name)
	}
	for i, value := range done {
		if slicesContain(done[:i], value) {
			return fmt.Sprintf("field %q names done value %q twice; one value is one value", f.Name, value)
		}
		if !slicesContain(f.Enum, value) {
			return fmt.Sprintf("field %q names done value %q, which its enum does not hold", f.Name, value)
		}
	}
	return ""
}

// moneyFault refuses the three shapes where a restated minor-unit scale could
// disagree with the data: a money format on a column that does not hold minor
// units, a scale with no currency to read, and a currency named by something
// other than a sibling string field. 0..3 is ISO 4217's minor-unit range, not a
// currency table — a currency table in the kernel would be a specialist fact in
// a shared module.
func moneyFault(name string, fields []crud.Field, f crud.Field) string {
	h, money := f.Presentation, f.Presentation.Money
	if h.Format != "money" && money == nil {
		return ""
	}
	if h.Format != "money" {
		return fmt.Sprintf("field %q names a currency field and is not formatted money", f.Name)
	}
	if f.Type != entity.TypeInt {
		return fmt.Sprintf("field %q is formatted money and is %s, which is not a minor-unit amount", f.Name, f.Type)
	}
	if money == nil {
		return fmt.Sprintf("field %q is formatted money and names no currency field; say which field carries the code", f.Name)
	}
	if money.Scale < 0 || money.Scale > 3 {
		return fmt.Sprintf("field %q reads money at scale %d; a minor-unit exponent is 0 to 3", f.Name, money.Scale)
	}
	if money.CurrencyField == "" {
		return fmt.Sprintf("field %q gives a money scale with no currency field to read the code from", f.Name)
	}
	if money.CurrencyField == f.Name {
		return fmt.Sprintf("field %q reads its currency from itself, which is an amount and not a code", f.Name)
	}
	// Note what this does not refuse: the currency field may be `hidden`. A code
	// is plumbing the reading needs, and a person usually has no use for it.
	currency, ok := entity.FieldNamed(fields, money.CurrencyField)
	if !ok || currency.Type != entity.TypeString {
		return fmt.Sprintf("field %q reads its currency from %q, which is not a string field of %s", f.Name, money.CurrencyField, name)
	}
	return ""
}

// commandFault checks a command's verb, presentation and argument ownership before
// registration. It returns a fault, or "" for a coherent declaration.
func commandFault(verb string, p entity.CommandHints, fields []crud.Field) string {
	bad := ""
	switch {
	case !verbSegment(verb):
		bad = fmt.Sprintf("command %q is not a path segment", verb)
	case p.Confirmation != nil && p.Confirmation.Title == "":
		bad = fmt.Sprintf("command %q declares a confirmation with no title", verb)
	case p.Primary && p.Destructive:
		bad = fmt.Sprintf("command %q is both primary and destructive; a screen cannot offer a button as the expected action and as the one that hurts", verb)
	case p.System && (p.Primary || p.Confirmation != nil || p.SuccessMessage != ""):
		bad = fmt.Sprintf("command %q is system and offers something anyway; nothing is offered, so nothing is primary", verb)
	}
	for _, long := range []struct {
		key, value string
	}{
		{"label", p.Label}, {"successMessage", p.SuccessMessage},
		{"confirmation title", confirmationTitle(p)},
		{"confirmation body", confirmationBody(p)},
		{"confirmation confirmLabel", confirmationLabel(p)},
	} {
		if len(long.value) > 200 {
			bad = fmt.Sprintf("command %q %s is %d bytes; a toast is not a page", verb, long.key, len(long.value))
		}
	}
	if bad != "" {
		return bad
	}
	for _, f := range fields {
		if entity.ServerOwned(f) {
			return fmt.Sprintf("argument field %q is server-owned; use a caller-owned argument name instead (ifRevision for an observed revision)", f.Name)
		}
	}
	// The argument's own hints run through the same gate. One consequence is
	// worth naming: an argument cannot read its currency from a sibling field of
	// the row, because an argument is not a row — it carries what the caller sent.
	return fieldHintFault(verb+" argument", fields, entity.EntryHints{})
}

func confirmationTitle(p entity.CommandHints) string {
	if p.Confirmation == nil {
		return ""
	}
	return p.Confirmation.Title
}

func confirmationBody(p entity.CommandHints) string {
	if p.Confirmation == nil {
		return ""
	}
	return p.Confirmation.Body
}

func confirmationLabel(p entity.CommandHints) string {
	if p.Confirmation == nil {
		return ""
	}
	return p.Confirmation.ConfirmLabel
}

// CheckReferences returns the first unresolved module/entity reference, or empty.
// Call after all resources are registered; both entity fields and command
// arguments must name a resource in that composed catalog.
func CheckReferences(resources []httpx.Resource) string {
	registered := map[string]bool{}
	for _, r := range resources {
		registered[resourceAddress(r.Module, r.Entity)] = true
	}
	for _, r := range resources {
		owner := resourceAddress(r.Module, r.Entity)
		if bad := unresolvedReference(owner+" field", r.Schema.Fields, registered); bad != "" {
			return bad
		}
		for _, c := range r.Commands {
			who := fmt.Sprintf("%s command %q argument field", owner, c.Verb)
			if bad := unresolvedReference(who, c.Fields, registered); bad != "" {
				return bad
			}
		}
	}
	return ""
}

// CheckRowHints checks row-action declarations against the complete registration.
// It delegates to the same gate used by API.ValidateDeclarations before boot.
func CheckRowHints(resources []httpx.Resource) string {
	return httpx.CheckRowHints(resources)
}

// unresolvedReference names the first field that points beyond the composition,
// saying which declaration named it so the author is not left to guess whether the
// row or one of its commands was the one reaching.
func unresolvedReference(declaredAs string, fields []entity.Field, registered map[string]bool) string {
	for _, f := range fields {
		ref := f.Presentation.Reference
		if ref == nil || registered[ref.Resource] {
			continue
		}
		return fmt.Sprintf("%s %q references %q, which no composed module registers", declaredAs, f.Name, ref.Resource)
	}
	return ""
}

// resourceAddress is the one way a resource is named as a thing a value can point
// at: `module/entity`. Both ends of a reference — the registration and the hint —
// are keyed by it, so the two can only ever meet.
func resourceAddress(module, entityName string) string { return module + "/" + entityName }

func lowerIdentifier(s string) bool {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for _, r := range s[1:] {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func verbSegment(s string) bool {
	for part := range strings.SplitSeq(s, "-") {
		if !lowerIdentifier(part) {
			return false
		}
	}
	return s != ""
}

func referenceTarget(s string) bool {
	module, target, ok := strings.Cut(s, "/")
	return ok && lowerIdentifier(module) && lowerIdentifier(target)
}

func hasSection(sections []entity.EntitySection, key string) bool {
	for _, sec := range sections {
		if sec.Key == key {
			return true
		}
	}
	return false
}

func slicesContain(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
