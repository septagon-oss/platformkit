package rest

import (
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/problem"
)

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
	return readForm(body, fields, formRead{refuse: refuse})
}

// CommandValues reads the form a command's arguments arrived in. It differs from
// Values in one thing and that one thing is the reason it is a name of its own: a
// command's argument struct may declare a field called entity.RevisionCondition,
// because quoting the revision the caller read the row at is what makes a command
// like a restore refuseable, and for a command that name is an argument to bind
// rather than the condition an entity form carries beside its fields. Read the
// other way round, the form that quotes a revision has its condition dropped and
// the write it was meant to guard commits — which is what this door did until a
// restore submitted from the generated form came back 303 instead of 412.
func CommandValues(body []byte, fields []crud.Field) (map[string]any, error) {
	return readForm(body, fields, formRead{arguments: true})
}

// formRead is how one of the three form doors reads the field list it was handed.
type formRead struct {
	// refuse names the fields that may not appear at all.
	refuse []string
	// update says this is an edit form: a submitted blank clears the value it
	// holds, and the form may carry the row's condition.
	update bool
	// arguments says the fields are a command's arguments. See CommandValues.
	arguments    bool
	markerRefuse []string
}

// UpdateValues reads an edit form, retaining submitted blank text, empty lists
// and null optional instants while omitting absent and command-owned fields. It
// also carries the row's condition: the revision the form was drawn from arrives
// under entity.RevisionCondition, and conditional turns it into the tag the write
// is made against.
func UpdateValues(body []byte, fields []crud.Field, immutable []string) (map[string]any, error) {
	values, err := readForm(body, fields, formRead{update: true, markerRefuse: immutable})
	if err != nil {
		return nil, err
	}
	return Writable(values, immutable), nil
}

func readForm(body []byte, fields []crud.Field, o formRead) (map[string]any, error) {
	form, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, problem.New(http.StatusUnprocessableEntity, "this form could not be read")
	}
	// Asked of every posted key before any field is read, so a key that folds
	// onto a refused name is refused and not merely left out of the lookup.
	if name := foldedName(form, o.refuse); name != "" {
		return nil, invalid(name, "belongs to a route of its own, not to this form")
	}
	if err := choiceMarkers(form, fields, append(slices.Clone(o.refuse), o.markerRefuse...)); err != nil {
		return nil, err
	}
	out := map[string]any{}
	for _, f := range fields {
		// The two kinds of field the server owns are read back as they are, never as
		// the form posted them: a control that carried the write count would otherwise
		// move the row to the number it showed.
		if entity.ServerOwned(f) {
			continue
		}
		if slices.Contains(o.refuse, f.Name) {
			continue
		}
		if f.Name == entity.RevisionCondition && !o.arguments {
			// Read once, by the code below, as the condition on the write rather than as
			// a field to merge. An entity that named a column this way would have its own
			// meaning for the name, which is why Spec.check refuses the collision, and a
			// command that names it means the argument, which is why a command's fields are
			// read with arguments set. See CommandValues.
			continue
		}
		if err := readFormField(form, f, o, out); err != nil {
			return nil, err
		}
	}
	// An edit form carries the revision it was drawn from, and an edit is the one write
	// that can be made against a row the person never saw. The name is read after the
	// fields rather than among them: it is no column's value, and a form that sent
	// nothing under it keeps the unconditional write every caller written before this
	// door has — the condition is how a screen says it read the row, not a tax it pays.
	if o.update {
		if raw, sent := form[entity.RevisionCondition]; sent {
			if text := strings.TrimSpace(raw[0]); text != "" {
				out[entity.RevisionCondition] = text
			}
		}
	}
	return out, nil
}

func readFormField(form url.Values, f crud.Field, o formRead, out map[string]any) error {
	if f.Choices != nil {
		return readChoice(form, f, o.update, out)
	}
	if f.Type == crud.TypeMap {
		keyed, err := formMap(form, f)
		if err != nil {
			return err
		}
		if len(keyed) > 0 {
			out[f.Name] = keyed
		}
		return nil
	}
	raw, sent := form[f.Name]
	if f.Type == crud.TypeBool {
		// An unticked checkbox's absence means false.
		out[f.Name] = sent && raw[0] != "" && raw[0] != "false"
		return nil
	}
	if !sent {
		return nil
	}
	text := strings.TrimSpace(raw[0])
	if text == "" {
		if o.update {
			clearFormField(f, out)
		}
		return nil
	}
	value, err := formValue(f, text)
	if err != nil {
		return err
	}
	out[f.Name] = value
	return nil
}

// Maps accept declared keys only; a blank control omits that key from the write.
func formMap(form url.Values, f crud.Field) (map[string]any, error) {
	keyed := map[string]any{}
	for _, k := range f.Keys {
		raw, sent := form[f.Name+"["+k.Name+"]"]
		if !sent {
			continue
		}
		text := strings.TrimSpace(raw[0])
		if text == "" {
			continue
		}
		typed, err := coerce(crud.Field{Name: f.Name + " " + k.Name, Type: f.Elem}, text)
		if err != nil {
			return nil, err
		}
		keyed[k.Name] = typed
	}
	return keyed, nil
}

func clearFormField(f crud.Field, out map[string]any) {
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

func formValue(f crud.Field, text string) (any, error) {
	switch f.Type {
	case crud.TypeInt:
		n, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return nil, invalid(f.Name, "is not a whole number")
		}
		return n, nil
	case crud.TypeFloat:
		n, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil, invalid(f.Name, "is not a number")
		}
		return n, nil
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
		return at.Format(time.RFC3339Nano), nil
	case crud.TypeList:
		return formList(f, text)
	default:
		return text, nil
	}
}

func formList(f crud.Field, text string) (any, error) {
	parts := strings.Split(text, ",")
	list := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			list = append(list, p)
		}
	}
	if f.Elem != crud.TypeInt && f.Elem != crud.TypeFloat && f.Elem != crud.TypeBool {
		return list, nil
	}
	typed := make([]any, 0, len(list))
	for _, value := range list {
		item, err := coerce(crud.Field{Name: f.Name, Type: f.Elem}, value)
		number, numeric := item.(float64)
		if err != nil || (numeric && (math.IsNaN(number) || math.IsInf(number, 0))) {
			return nil, invalid(f.Name, "contains an invalid "+string(f.Elem))
		}
		typed = append(typed, item)
	}
	return typed, nil
}
