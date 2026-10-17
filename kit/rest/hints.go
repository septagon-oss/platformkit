package rest

import (
	"fmt"
	"strings"

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
	return entryHintsFault(s.Entity, s.Present, crud.Fields[T]())
}

func entryHintsFault(name string, p entity.EntryHints, fields []crud.Field) string {
	if !entity.ValidIcon(p.Icon) {
		return fmt.Sprintf("icon %q is not an icon; entity.Icons admits %s", p.Icon, strings.Join(entity.Icons, ", "))
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
			return fmt.Sprintf("field %q is formatted %q, which is not a format entity.Formats admits", f.Name, h.Format)
		}
		if !entity.ValidVisibility(h.Visibility) {
			return fmt.Sprintf("field %q declares visibility %q, which is not a visibility entity.Visibilities admits", f.Name, h.Visibility)
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
				return fmt.Sprintf("field %q names tone %q for value %q, which is not a tone entity.Tones admits", f.Name, said, value)
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

// commandFault names what one command's hints get wrong, and "" when they are
// coherent. It runs inside rest.Command rather than Spec.check because a command
// is the one place the verb, its options, the entity's immutable list and the
// argument's fields are all in hand at once.
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

// CheckReferences is the boot gate for a `reference`: it answers whether every
// `module/entity` a composed resource names is a resource this installation
// actually registered, and "" when they all are. kit/app calls it once the whole
// resource list exists, which is the first moment the question has an answer.
//
// Two declarations carry a reference: a field of the entity, and a field of a
// command's argument. The first is the one a row holds and the second the one a
// person sends, and the catalogue publishes both, so the gate reads both — a
// target gate that walked only the entity's schema would let a command offer to
// point a value at a resource nobody composed.
//
// One spelling throughout: `module/entity`, what `referenceTarget` refuses every
// other shape of at mount and what the catalogue publishes. A gate that keyed the
// registry by another spelling would refuse every reference a mounted Spec is
// allowed to write, which is the same bug as not checking at all, told louder.
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

// CheckRowHints is the boot gate for the entry hints that speak of a row and a
// verb: it answers whether a resource's `primaryCommand` names a command this
// resource actually mounts, and one a swipe could run, and "" when they all do.
//
// It is asked at the same moment as CheckReferences, and for the same reason:
// `entryHintsFault` runs in a Spec's own mount check, where no command exists yet
// — commands arrive later, through a separate registration — so the first site
// where an entry's declaration and its resource's verbs are in hand together is
// the one that has the whole resource list. The refusal stays out of the command's
// own gate, which runs from the verb's side and could never see a verb that was
// never mounted.
//
// Three things make a mounted verb one a row cannot swipe to: it asks for values,
// which a swipe sends none of (an argument is declared before the route is, so
// this reads the fields a map-only argument would have produced and is not
// satisfied by one that hides them), it is a system verb, which no person is
// offered and which mounts no browser route at all, or it runs over the whole
// collection, where a swipe names one row.
func CheckRowHints(resources []httpx.Resource) string {
	for _, r := range resources {
		if r.Present.PrimaryCommand == "" {
			continue
		}
		owner := resourceAddress(r.Module, r.Entity)
		verb, found := namedCommand(r.Commands, r.Present.PrimaryCommand)
		if !found {
			return fmt.Sprintf("%s primaryCommand %q is not a command of %s", owner, r.Present.PrimaryCommand, r.Entity)
		}
		switch {
		case len(verb.Fields) > 0:
			return fmt.Sprintf("%s primaryCommand %q takes arguments; a row's swipe sends none", owner, verb.Verb)
		case verb.Present.System:
			return fmt.Sprintf("%s primaryCommand %q is a system command; no person is offered it", owner, verb.Verb)
		case verb.Collection:
			return fmt.Sprintf("%s primaryCommand %q runs on the collection; a row's swipe names one row", owner, verb.Verb)
		}
	}
	return ""
}

// namedCommand is the verb a hint names, if this resource mounts one at all.
func namedCommand(commands []httpx.Command, verb string) (httpx.Command, bool) {
	for _, c := range commands {
		if c.Verb == verb {
			return c, true
		}
	}
	return httpx.Command{}, false
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
