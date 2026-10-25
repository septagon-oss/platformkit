package view

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/display"
	"github.com/septagon-oss/platformkit/kit/entity/present"
)

// ValueKind is which member of the value domain a Page wrote for one value. The field
// decides it, except where the value's magnitude is the fact — an integer past what a
// double holds — and there the Page says which it wrote, so a builder switches on the
// Page and never on its own guess.
type ValueKind string

// The value domain. `raw` is the one kind with no domain member: a value JSON cannot
// spell keeps its text and nulls its value, because a read is never lost over a
// spelling.
const (
	ValueAbsent     ValueKind = "absent"
	ValueRaw        ValueKind = "raw"
	ValueString     ValueKind = "string"
	ValueBoolean    ValueKind = "boolean"
	ValueInteger    ValueKind = "integer"
	ValueBigInteger ValueKind = "bigInteger"
	ValueDecimal    ValueKind = "decimal"
	ValueMoney      ValueKind = "money"
	ValueInstant    ValueKind = "instant"
	ValueDate       ValueKind = "date"
	ValuePicture    ValueKind = "picture"
	ValueList       ValueKind = "list"
	ValueMap        ValueKind = "map"
)

// The two spellings a Page fixes a value to. maxSafeInteger is 2^53-1, the largest
// integer a double holds exactly, so an integer of either sign past it is a canonical
// decimal string of the same digits; instantLayout and dateLayout are fixed so a
// shell parses a time without guessing what a shorter spelling meant.
const (
	maxSafeInteger = 1<<53 - 1
	instantLayout  = "2006-01-02T15:04:05.000000Z"
	dateLayout     = "2006-01-02"
)

// fieldReader resolves one field's words, colour and value out of the policy and
// the schema for one zone, so every method is a lookup and a spelling.
type fieldReader struct {
	zone   *time.Location
	fields map[string]present.ResolvedField
	schema map[string]entity.Field
}

func newReader(in Input) fieldReader {
	r := fieldReader{
		zone:   in.State.Zone,
		fields: make(map[string]present.ResolvedField, len(in.Policy.Fields)),
		schema: make(map[string]entity.Field, len(in.Resource.Schema.Fields)),
	}
	if r.zone == nil {
		r.zone = time.UTC
	}
	for _, f := range in.Policy.Fields {
		r.fields[f.Name] = f
	}
	for _, f := range in.Resource.Schema.Fields {
		r.schema[f.Name] = f
	}
	return r
}

// lookup resolves a layout or field name to the pair a page shows it from. A name
// the schema has no field for is skipped rather than invented: the layout is the
// policy's, the schema is the truth about what can be read. A field the policy says
// nothing about keeps display's plain reading of it.
func (r fieldReader) lookup(name string) (present.ResolvedField, entity.Field, bool) {
	sf, ok := r.schema[name]
	if !ok {
		return present.ResolvedField{}, sf, false
	}
	rf, resolved := r.fields[name]
	if !resolved {
		rf = present.ResolvedField{Name: name, Label: display.FieldLabel(sf), Help: display.FieldHelp(sf)}
	}
	return rf, sf, true
}

// field is one field as a record shows it.
func (r fieldReader) field(name string, row map[string]any) (Field, bool) {
	rf, sf, ok := r.lookup(name)
	if !ok {
		return Field{}, false
	}
	kind, elem, value := r.value(rf, sf, row)
	f := Field{Name: sf.Name, Label: rf.Label, Help: rf.Help, Text: r.text(rf, sf, row),
		Tone: r.tone(rf, row), ValueKind: kind, Elem: elem, Value: value}
	if rf.Reference != nil {
		f.Reference = rf.Reference.Resource
	}
	return f, true
}

// cell is one field of one row; its six reading members mean what Field's mean.
func (r fieldReader) cell(name string, row map[string]any) (Cell, bool) {
	f, ok := r.field(name, row)
	if !ok {
		return Cell{}, false
	}
	return Cell{Field: f.Name, Label: f.Label, Text: f.Text, Tone: f.Tone,
		ValueKind: f.ValueKind, Elem: f.Elem, Value: f.Value}, true
}

// text is a field's value as a person reads it: the policy's words for an absent
// value, a boolean and a declared enum value, display's reading of an instant, and the
// stored text of everything else. An enum value nobody declared keeps its own text:
// the humanising display.EnumWord does is a renderer's guess, not a fact.
func (r fieldReader) text(rf present.ResolvedField, sf entity.Field, row map[string]any) string {
	raw, ok := row[sf.Name]
	if !ok || raw == nil {
		return rf.Reading.Missing
	}
	switch {
	case sf.Type == entity.TypeBool:
		if b, isBool := raw.(bool); isBool {
			if b {
				return rf.Reading.True
			}
			return rf.Reading.False
		}
	case len(rf.Options) > 0:
		if o, declared := optionOf(rf.Options, textOf(raw)); declared {
			return o.Label
		}
		return textOf(raw)
	case rf.Reading.Time != nil:
		if _, out, ok := display.Moment(raw); ok {
			return out
		}
	}
	return textOf(raw)
}

// tone is the colour the author declared for this value, "" for a field that
// declares none, and "neutral" for a value outside the declared set: a value
// nobody named may not wear the colour of the one that reads nearest it.
func (r fieldReader) tone(rf present.ResolvedField, row map[string]any) string {
	if len(rf.Options) == 0 {
		return ""
	}
	raw, ok := row[rf.Name]
	if !ok || raw == nil {
		return ""
	}
	if o, declared := optionOf(rf.Options, textOf(raw)); declared {
		return o.Tone
	}
	return "neutral"
}

// value decides the kind and the JSON spelling of one field's value. The field
// decides the kind; only an integer's magnitude lets the value decide.
func (r fieldReader) value(rf present.ResolvedField, sf entity.Field, row map[string]any) (ValueKind, ValueKind, any) {
	raw, ok := row[sf.Name]
	if !ok || raw == nil {
		return ValueAbsent, "", nil
	}
	if m := rf.Reading.Money; m != nil {
		return ValueMoney, "", Money{MinorUnits: amountOf(raw), Scale: m.Scale,
			Currency: textOf(row[m.CurrencyField])}
	}
	switch sf.Type {
	case entity.TypeBool:
		if b, isBool := raw.(bool); isBool {
			return ValueBoolean, "", b
		}
	case entity.TypeInt:
		return number(raw)
	case entity.TypeFloat:
		return decimal(raw)
	case entity.TypeTime:
		return r.temporal(rf, raw)
	case entity.TypePicture:
		return ValuePicture, "", textOf(raw)
	case entity.TypeList:
		return ValueList, elemKind(sf.Elem), elements(sf.Elem, raw)
	case entity.TypeMap:
		return ValueMap, "", mapping(raw)
	}
	return ValueString, "", textOf(raw)
}

// temporal is an instant or a calendar day. The words stay display.Moment's; the
// value is the instant itself with six fractional digits, or the day cut in the
// zone the caller loaded, which is the only zone this package may use.
func (r fieldReader) temporal(rf present.ResolvedField, raw any) (ValueKind, ValueKind, any) {
	at := instant(raw)
	if at.IsZero() {
		return ValueRaw, "", nil
	}
	if rf.Reading.Time != nil && rf.Reading.Time.Kind == string(ValueDate) {
		return ValueDate, "", at.In(r.zone).Format(dateLayout)
	}
	return ValueInstant, "", at.UTC().Format(instantLayout)
}

// elements spells a list's elements in declared order. A nil list is []: the only
// nulls in a Page are field values, and an empty set is a fact a shell can print. An
// element carries its declared type's own spelling, with no money, no time and no zone
// to consult: a list declares a type, not a field.
func elements(elem entity.FieldType, raw any) []any {
	var list []any
	switch typed := raw.(type) {
	case []any:
		list = typed
	case []string:
		for _, item := range typed {
			list = append(list, item)
		}
	}
	out := make([]any, 0, len(list))
	for _, item := range list {
		out = append(out, element(elem, item))
	}
	return out
}

func element(elem entity.FieldType, item any) any {
	if item == nil {
		return nil
	}
	switch elem {
	case entity.TypeBool:
		if b, ok := item.(bool); ok {
			return b
		}
	case entity.TypeInt:
		if _, _, out := number(item); out != nil {
			return out
		}
	case entity.TypeFloat:
		if _, _, out := decimal(item); out != nil {
			return out
		}
	}
	return textOf(item)
}

// mapping is a map value as it stands: encoding/json writes its keys sorted, which
// is the order the Page carries, so no sort happens here and no key is dropped.
func mapping(raw any) map[string]any {
	m, ok := raw.(map[string]any)
	if !ok || m == nil {
		return map[string]any{}
	}
	// A Page is a value: the map it shows is a copy, so a shell that holds one never
	// watches the caller's own write change it.
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// number is an integer: a JSON number while it fits a double exactly, and a canonical
// decimal string past it in either sign. A value that arrived as a float64 is one some
// caller already decoded as a float, so it is spelled decimal — the shape stays safe
// and the digits are the ones that survived that decode.
func number(raw any) (ValueKind, ValueKind, any) {
	switch typed := raw.(type) {
	case int:
		return integer(int64(typed))
	case int32:
		return integer(int64(typed))
	case int64:
		return integer(typed)
	case json.Number:
		if n, err := strconv.ParseInt(typed.String(), 10, 64); err == nil {
			return integer(n)
		}
		return decimal(typed)
	case float64:
		return decimal(typed)
	case string:
		if n, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64); err == nil {
			return integer(n)
		}
	}
	return ValueString, "", textOf(raw)
}

// integer is a whole number: a JSON number within the safe bound of the double
// every other client has, a decimal string of the same digits past it.
func integer(v int64) (ValueKind, ValueKind, any) {
	if v > maxSafeInteger || v < -maxSafeInteger {
		return ValueBigInteger, "", strconv.FormatInt(v, 10)
	}
	return ValueInteger, "", v
}

// decimal is a number that is not a whole one: a canonical decimal string always,
// and `raw` for a number with no decimal spelling at all.
func decimal(raw any) (ValueKind, ValueKind, any) {
	switch typed := raw.(type) {
	case float64:
		return finite(typed)
	case float32:
		return finite(float64(typed))
	case json.Number:
		f, err := strconv.ParseFloat(typed.String(), 64)
		if err != nil {
			return ValueRaw, "", nil
		}
		return finite(f)
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return ValueString, "", textOf(raw)
		}
		return finite(f)
	}
	return ValueRaw, "", nil
}

// finite is a float's own decimal spelling, or `raw` for one that has none.
func finite(f float64) (ValueKind, ValueKind, any) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return ValueRaw, "", nil
	}
	return ValueDecimal, "", strconv.FormatFloat(f, 'f', -1, 64)
}

// amountOf is the canonical decimal spelling of a money amount: no '+', no leading
// zero, no separator and no exponent, in both signs. A double never carries money, so
// the digits a client reads are the digits that were stored.
func amountOf(raw any) string {
	switch typed := raw.(type) {
	case int:
		return strconv.FormatInt(int64(typed), 10)
	case int32:
		return strconv.FormatInt(int64(typed), 10)
	case int64, json.Number:
		return fmt.Sprint(typed)
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return ""
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	}
	return textOf(raw)
}

// instant is an instant from whatever a read carried: the wire text, or a Go time
// for the caller that handed over its own value rather than a decoded one.
func instant(raw any) time.Time {
	if at, isTime := raw.(time.Time); isTime {
		return at.UTC()
	}
	if at, ok := display.Instant(raw); ok {
		return at
	}
	return time.Time{}
}

// elemKinds is the value kind each declared element type spells: the same domain,
// decided by the type alone, with no field to read money or a time reading from.
var elemKinds = map[entity.FieldType]ValueKind{
	entity.TypeInt: ValueInteger, entity.TypeFloat: ValueDecimal, entity.TypeBool: ValueBoolean,
	entity.TypeTime: ValueInstant, entity.TypeList: ValueList, entity.TypeMap: ValueMap,
	entity.TypePicture: ValuePicture,
}

// elemKind is an element's kind, and `string` for a type the domain names nothing
// for.
func elemKind(t entity.FieldType) ValueKind {
	if kind, ok := elemKinds[t]; ok {
		return kind
	}
	return ValueString
}

// textOf is a value as a form control and a link read it: display.Text, plus the
// carriers that reach a page from a Go handler rather than a JSON decode. A map's
// text is its own JSON, the only honest text for a value with no reading of its
// own: it shows what is stored rather than nothing.
func textOf(v any) string {
	switch typed := v.(type) {
	case int64:
		return strconv.FormatInt(typed, 10)
	case int:
		return strconv.FormatInt(int64(typed), 10)
	case time.Time:
		return typed.UTC().Format(instantLayout)
	case map[string]any:
		if raw, err := json.Marshal(typed); err == nil {
			return string(raw)
		}
		return fmt.Sprint(typed)
	}
	return display.Text(v)
}

// optionOf is the option an author declared for a value, and whether they declared
// one at all.
func optionOf(options []present.ResolvedOption, value string) (present.ResolvedOption, bool) {
	for _, o := range options {
		if o.Value == value {
			return o, true
		}
	}
	return present.ResolvedOption{}, false
}

// revisionOf is a row's own write counter, or 0 when the read carried none. Past the
// safe bound the counter is still its own digits, so the revision a page hands back is
// the one the row holds.
func revisionOf(row map[string]any) int64 {
	kind, _, value := number(row[entity.RevisionField])
	switch {
	case kind == ValueInteger:
		n, _ := value.(int64)
		return n
	case kind == ValueBigInteger:
		if s, ok := value.(string); ok {
			n, _ := strconv.ParseInt(s, 10, 64)
			return n
		}
	}
	return 0
}

// idOf is a row's id as the list names it; a singleton's row genuinely has none.
func idOf(row map[string]any) string {
	if row == nil {
		return ""
	}
	return strings.TrimSpace(display.Text(row["id"]))
}

// namesOrNil is the [] a Page writes for "none": a nil slice would be a null, and
// a null that means "empty" is the second shape this contract refuses.
func namesOrNil[T any](in []T) []T {
	if in == nil {
		return []T{}
	}
	return slices.Clone(in)
}
