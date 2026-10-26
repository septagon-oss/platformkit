package rest

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/internal/jsonbody"
)

// merge applies a PATCH body to an entity, field by field, through the schema,
// and reports the database columns it changed so that Update writes those and
// no others. A name the schema does not know, one it knows as read-only, or one
// that folds onto a name the Spec reserved is refused rather than ignored,
// because a caller who spells a field wrong — or reaches for the wrong door —
// has to be told.
func merge(e any, fields []crud.Field, immutable []string, patch map[string]any) ([]string, error) {
	// The reserved names are asked of the whole body first, so the answer does
	// not depend on map order, and folded, because this map is a decoded body:
	// encoding/json binds "Status" into Status, and the exact lookup below
	// would answer "there is no field" about a field that is there.
	if name := foldedName(patch, immutable); name != "" {
		return nil, immutableRefusal(name)
	}
	target := reflect.ValueOf(e).Elem()
	columns := make([]string, 0, len(patch))
	for name, value := range patch {
		f, ok := crud.FieldNamed(fields, name)
		switch {
		case !ok:
			return nil, fmt.Errorf("%w: there is no field %q", crud.ErrInvalid, name)
			// Both kinds of server-owned name are refused here, in one question, with
			// sentences that differ only in what the caller can do next: a base field is
			// somebody else's, and the write counter moves because a write happened.
		case entity.ServerOwned(f):
			return nil, serverOwnedRefusal(name)
		}
		// Binding through the private codec is what makes this the same decoder the
		// request body went through: one set of rules for "3" as an int, for null as
		// an empty pointer, and for the digits of an int64.
		field := target
		for _, index := range f.Index {
			if field.Kind() == reflect.Pointer {
				if field.IsNil() {
					if !field.CanSet() {
						return nil, fmt.Errorf("%w: %s cannot initialize an unexported embedded field", crud.ErrInvalid, name)
					}
					field.Set(reflect.New(field.Type().Elem()))
				}
				field = field.Elem()
			}
			field = field.Field(index)
		}
		if err := jsonbody.Bind(value, field.Addr().Interface()); err != nil {
			return nil, fmt.Errorf("%w: %s: %s", crud.ErrInvalid, name, err)
		}
		columns = append(columns, f.Column)
	}
	return columns, nil
}

// serverOwnedRefusal is the answer at every write door that takes a field the server
// owns. The two sentences differ because the caller's next step differs: a base field
// belongs to another door, and the write counter belongs to no door at all — it moves
// when something else is written, which is the whole of what makes a stale write
// refuseable.
func serverOwnedRefusal(name string) error {
	if name == RevisionField {
		return fmt.Errorf("%w: %s is this row's own write count: it moves with a write and is not one to set",
			crud.ErrInvalid, name)
	}
	return fmt.Errorf("%w: %s is read-only", crud.ErrInvalid, name)
}

// refuseStaleTag answers a conditional write whose tag the row has passed.
//
// The number the caller is told is the current one, read under the same row lock the
// write would have taken, because the only useful thing a 412 can say is "here is what
// it is on now" — a client that has to re-read to find out is a client that will send
// the old tag again under a race. Absent If-Match is allowed everywhere: the tag is how
// a screen that read the row says so, and every client that never read one (the checked
// -in documents, the mobile flows, every caller written before this door) keeps working.
func refuseStaleTag(ifMatch string, current int64) error {
	if ifMatch == "" {
		return nil
	}
	if current == 0 {
		// Nothing can quote a resource that keeps no write count, and the honest answer
		// is the one that says so rather than a 412 about a tag nobody ever sent or a
		// silence that wrote whatever arrived. This is why the ETag header above is
		// carried only by the Specs whose entity has the field: the parameter is one
		// mount for every Spec, and this is the half of the sentence its doc says.
		return fmt.Errorf("%w: this resource carries no write count, so no tag can be quoted against it",
			crud.ErrInvalid)
	}
	sent, err := parseTag(ifMatch)
	if err != nil {
		return err
	}
	if sent == current {
		return nil
	}
	return fmt.Errorf("%w: this row is on revision %d, and the tag this write quoted (%s) is not it",
		ErrStaleTag, current, ifMatch)
}

// parseTag reads the one form this door honours: one quoted integer, the ETag it wrote.
//
// A weak tag, a list, a bare number and `*` are all refused 422 rather than ignored.
// `*` — "whatever is there now" — is refused here in particular because a conditional
// write that promises nothing about the row is not conditional, and a reviewer's
// approval is about one revision of one row.
func parseTag(raw string) (int64, error) {
	unquoted := strings.TrimPrefix(strings.TrimSuffix(strings.TrimSpace(raw), "\""), "\"")
	if unquoted == raw || unquoted == "" || strings.HasPrefix(strings.TrimSpace(raw), "W/") {
		return 0, fmt.Errorf("%w: %q is not one quoted revision, the only tag this door writes",
			crud.ErrInvalid, raw)
	}
	tag, err := strconv.ParseInt(unquoted, 10, 64)
	if err != nil || tag < 1 {
		return 0, fmt.Errorf("%w: %q is not one quoted revision, the only tag this door writes",
			crud.ErrInvalid, raw)
	}
	return tag, nil
}

// refuseImmutable answers 422 when a create body names a field a route of its
// own owns.
//
// It reads the raw body rather than the decoded entity, because the two are not
// the same question: a decoded entity cannot say whether the caller sent
// "author": null, "author": "0000…" or nothing at all, and only the last is
// allowed. So the create route asks for the bytes as well as the struct — huma
// fills both from one read — and foldedName names what it refuses.
//
// The refusal is the patch's, word for word, because it is the same rule: a
// field a command owns is written by that command at every door. Read-only
// fields are not this; those the server owns outright and crud.Reset discards
// whatever arrived for them, which is right, because a caller sending an id is
// not reaching for a door of its own.
func refuseImmutable(body []byte, immutable []string) error {
	if len(immutable) == 0 || len(body) == 0 {
		return nil
	}
	var sent map[string]json.RawMessage
	if err := json.Unmarshal(body, &sent); err != nil {
		// A body that is not an object is huma's refusal to give, not this
		// one's: it has already failed to decode into the entity.
		return nil
	}
	if name := foldedName(sent, immutable); name != "" {
		return immutableRefusal(name)
	}
	return nil
}

// foldedName is the one question every write door asks of the top-level keys it
// was handed — a request's bytes after one read, the map that decoded into a
// struct, the form a browser posted — and asks it the way the decoder does:
// strings.EqualFold, the Unicode simple case folding encoding/json binds a field
// by when no key matches exactly, long s included. A map lookup is not that
// question, and the decoder is the authority: a key it binds into a command's
// field has to be refused by the door in front of it. It answers with the
// declared field, or "" when the body names none; of two, the Spec's order wins.
func foldedName[V any](sent map[string]V, immutable []string) string {
	for _, name := range immutable {
		for key := range sent {
			if strings.EqualFold(key, name) {
				return name
			}
		}
	}
	return ""
}

// immutableRefusal is the one message every door but the form's gives, so a
// caller who reaches for a field a command owns is told the same thing whether
// they created or patched, over the wire or beneath the HTTP.
func immutableRefusal(name string) error {
	return fmt.Errorf("%w: %s belongs to a route of its own, not to this one", crud.ErrInvalid, name)
}

// names are the fields the list route can be sorted and filtered by, which is
// every field but a list: a column holding many values is not one an equality
// compares against, and a document that offered it would be describing a 422.
func names(fields []crud.Field) []string {
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f.Type != crud.TypeList && f.Type != entity.TypePicture {
			out = append(out, f.Name)
		}
	}
	return out
}
