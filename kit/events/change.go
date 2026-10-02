package events

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// Change is one field of one row as it moved. The trail stores payloads verbatim
// and will not start normalising them (modules/audit/migrations/000010 says so), so
// "every audited change carries what changed" is the emitting module's contract, and
// this is the one unit that keeps every module from hand-rolling its own diff.
//
// The names are the payload's own json names — homeSlug, not HomeSlug — because the
// trail is read as json and a second spelling of a field name is two answers that can
// disagree. A value nobody may copy — a credential, a body, anything whose disclosure
// is the incident — arrives as its digest: the field is tagged `audit:"digest"`, and
// both halves come out as "sha256:<hex>" of the marshalled value, which lets somebody
// who holds the value confirm which one it was without the table holding it for a year.
type Change struct {
	Field  string          `json:"field"`
	Before json.RawMessage `json:"before,omitempty"`
	After  json.RawMessage `json:"after,omitempty"`
}

// Changes returns the fields a save moved, over the names the caller lists.
//
// Both operands are *T, which is the compiler refusing a diff of two different rows —
// the mistake this makes impossible is comparing a site settings row against a task
// row and calling the result history. A field is changed when the json of its value
// differs byte for byte: one comparison, no second equality logic to drift from the
// type, and a time.Time therefore compares to the nanosecond it stores.
//
// fields is an allow-list, not "every field", and that is the same decision the
// emitting module already makes about the columns a save writes: a column nobody names
// is not diffed, and an unlisted column that moves is a hole a test finds rather than a
// silent omission. A name the type does not carry is an error rather than a skipped
// name, because a list that quietly stops covering a field is a diff that quietly stops
// explaining the save, and rule 9 means the caller's mutation fails and writes nothing.
//
// before == nil is a create: every named field that carries a value is new, and says so
// with only its After half. A field tagged `audit:"-"` may not be asked for at all.
func Changes[T any](before, after *T, fields ...string) ([]Change, error) {
	if after == nil {
		return nil, fmt.Errorf("events: a change has to say what the row moved to")
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("events: a diff over no fields explains nothing")
	}
	a := reflect.ValueOf(*after)
	if a.Kind() != reflect.Struct {
		return nil, fmt.Errorf("events: %T is not a row", after)
	}
	var b reflect.Value
	if before != nil {
		b = reflect.ValueOf(*before)
	}
	var out []Change
	for _, name := range fields {
		f, ok := fieldByJSON(a.Type(), name)
		if !ok {
			return nil, fmt.Errorf("events: %s has no field with the json name %q", a.Type().Name(), name)
		}
		tag := f.Tag.Get("audit")
		switch tag {
		case "-":
			return nil, fmt.Errorf("events: %s.%s is tagged audit:\"-\": a field nothing may record may not be asked for", a.Type().Name(), f.Name)
		case "", "digest":
			afterJSON, err := json.Marshal(a.FieldByIndex(f.Index).Interface())
			if err != nil {
				return nil, fmt.Errorf("events: %s.%s: %w", a.Type().Name(), f.Name, err)
			}
			if tag == "digest" {
				afterJSON = digested(afterJSON)
			}
			c := Change{Field: name, After: afterJSON}
			if !b.IsValid() {
				if !zeroJSON(afterJSON) {
					out = append(out, c)
				}
				continue
			}
			beforeJSON, err := json.Marshal(b.FieldByIndex(f.Index).Interface())
			if err != nil {
				return nil, fmt.Errorf("events: %s.%s: %w", a.Type().Name(), f.Name, err)
			}
			if tag == "digest" {
				beforeJSON = digested(beforeJSON)
			}
			if string(beforeJSON) == string(afterJSON) {
				continue
			}
			// A json null is an absence, not a value: an omitted pointer or an absent
			// optional leaves no before half, and a trail row that said "from null"
			// would be a claim about a value the row never held.
			if !zeroJSON(beforeJSON) {
				c.Before = beforeJSON
			}
			out = append(out, c)
		default:
			return nil, fmt.Errorf("events: %s.%s carries the unknown audit tag %q", a.Type().Name(), f.Name, tag)
		}
	}
	return out, nil
}

// fieldByJSON is the struct's own field for a json name, through embedded structs the
// way encoding/json does. crud.Base is where a row's revision and timestamps live, and a
// diff that could not name them would be a diff that silently covered less than the row.
func fieldByJSON(t reflect.Type, name string) (reflect.StructField, bool) {
	for _, f := range reflect.VisibleFields(t) {
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			continue
		}
		if jsonName(f) == name {
			return f, true
		}
	}
	return reflect.StructField{}, false
}

// jsonName is the name encoding/json would write: the tag, or the Go name when the tag
// says nothing, and "-" means the field is not in the payload at all.
func jsonName(f reflect.StructField) string {
	tag := f.Tag.Get("json")
	if name, _, _ := strings.Cut(tag, ","); name != "" {
		return name
	}
	if tag == "-" {
		return ""
	}
	return f.Name
}

// digested is the "or its digest" the trail accepts instead of the value: SHA-256 over
// the marshalled json, rendered as a json string so the payload stays a payload.
func digested(value []byte) json.RawMessage {
	sum := sha256.Sum256(value)
	out, err := json.Marshal("sha256:" + fmt.Sprintf("%x", sum))
	if err != nil {
		// json.Marshal of a string cannot fail; the alternative is returning a
		// half-built Change, which is the thing this function exists to avoid.
		panic("events: a digest is not a json string: " + err.Error())
	}
	return out
}

// zeroJSON reports the marshalled value as an absence: "", 0, false, null, [] or {}. A
// create carries no news in a field the new row does not fill.
func zeroJSON(v []byte) bool {
	switch s := string(v); s {
	case `""`, `0`, `false`, `null`, `[]`, `{}`:
		return true
	default:
		return false
	}
}
