package resource

import (
	"encoding/json"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/display"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	"github.com/septagon-oss/platformkit/ui/forms"
	g "maragu.dev/gomponents"
)

// FormExample captures Form's body for an owner-local example ID and synthetic
// row/errors. Field slots use "field/<name>"; action scopes the DOM across swaps.
// Invalid portable form metadata panics. Property edits change presentation only.
func FormExample(id string, r Resource, o Options, action, title string, row map[string]any, errs map[string]string, detail string, create bool) examples.Example {
	// Detail fields remain editable; hidden fields do not become successful inputs.
	// Reserve one extra slot for the edit's revision condition.
	fields := make([]forms.Field, 0, len(r.Schema.Fields)+1)
	policy := r.policy(o)
	choiceValues := map[string][]string{}
	words := o.Words(r.Schema)
	// A refusal keeps the submitted values when rebuilding the editor.
	refused := detail != ""
	for _, field := range readable(r.Schema.Fields) {
		f := formField(words, field, o)
		if field.Choices != nil {
			for _, resolved := range policy.Fields {
				if resolved.Name == field.Name {
					f.Resolved = new(resolved)
				}
			}
			if value, exists := row[field.Name]; exists {
				choiceValues[field.Name] = readChoiceIDs(field, value)
			}
		}
		if f.Definition.Widget == "richtext" {
			f.Editor = richtextEditor(r, row, create, f.Definition.Name, refused)
		}
		fields = append(fields, f)
	}
	values := make(map[string]string, len(row))
	for name, value := range row {
		values[name] = display.Text(value)
	}
	// Keep the revision originally read, including after a refused write, so a
	// resubmission cannot silently replace a newer row.
	if condition, quoted := revisionCondition(r, row, create); quoted {
		fields = append(fields, forms.Field{Definition: entity.Field{
			Name: entity.RevisionCondition, Type: entity.TypeString, Widget: "hidden",
			Doc: "The revision this form was drawn from. The write is refused if the row has moved.",
		}})
		values[entity.RevisionCondition] = condition
	}
	return forms.MustExample(id, forms.Model{ChoiceValues: choiceValues, Fields: fields, Values: values, Errors: errs,
		Immutable: r.Immutable, Detail: detail, Create: create}, forms.Options{
		// The address the form posts to is its DOM scope: see forms.Namespace.
		Namespace: forms.Namespace(action),
		Action:    action, CancelURL: r.Screen, Title: title,
		Text: func(key, fallback string) string {
			if o.Locale == nil {
				return fallback
			}
			return o.Locale.Text(key, fallback)
		},
		// Empty refusal words retain the portable form's default heading and tone.
		FailureTitle: refusalHeading(o), DetailTone: refusalTone(o),
	})
}

// refusalHeading keeps the form's default heading when no refusal was supplied.
func refusalHeading(o Options) string {
	if o.Refusal == nil {
		return ""
	}
	return o.Refusal.Heading
}

func refusalTone(o Options) string {
	if o.Refusal == nil {
		return ""
	}
	return o.Refusal.Tone
}

// revisionCondition quotes a declared revision from a read or refused form.
// Creates and singleton PUTs have no supported revision condition.
func revisionCondition(r Resource, row map[string]any, create bool) (string, bool) {
	if create || r.Singleton || row == nil {
		return "", false
	}
	counts := false
	for _, f := range r.Schema.Fields {
		if f.Name == entity.RevisionField && f.Type == entity.TypeInt {
			counts = true
		}
	}
	if !counts {
		return "", false
	}
	if count, has := row[entity.RevisionField]; has {
		return display.Text(count), true
	}
	if carried, has := row[entity.RevisionCondition]; has {
		return display.Text(carried), true
	}
	return "", false
}

// Control adapts an entity field to the portable form control, preserving the
// generated screens' existing labels, choices and name-derived DOM identity.
func Control(f entity.Field, value, fieldErr string, immutable bool) g.Node {
	return forms.Control(forms.ControlProps{Field: formField(Words{}, f, Options{}), Value: value, Error: fieldErr, Immutable: immutable})
}

// formField projects localized words and resolved choice policy for a control.
// Zero Words keeps the declaration's original labels.
func formField(words Words, f entity.Field, o Options) forms.Field {
	options := make([]components.SelectOption, 0, len(f.Enum))
	for _, value := range f.Enum {
		options = append(options, components.SelectOption{Label: words.EnumWord(f, value), Value: value})
	}
	out := forms.Field{PictureSource: o.PictureSource, ComposedSource: o.ComposedSource, Definition: f, Label: words.FieldLabel(f), Help: words.FieldHelp(f), Options: options}
	if f.Choices != nil {
		local := words.Fields([]entity.Field{f})
		resolved := present.Resolve(present.Input{Schema: entity.Schema{Fields: local}})
		out.Resolved = &resolved.Fields[0]
	}
	return out
}

func readChoiceIDs(f entity.Field, value any) []string {
	if value == nil {
		return []string{}
	}
	ids, err := f.Choices.ChoiceIDs(value)
	if err != nil {
		return []string{}
	}
	return ids
}

func choiceDefault(f entity.Field) []string {
	if f.Default == "" {
		return nil
	}
	value, err := entity.DefaultValue(f)
	if err != nil {
		return nil
	}
	if raw, ok := value.(json.RawMessage); ok {
		_ = json.Unmarshal(raw, &value)
	}
	return readChoiceIDs(f, value)
}
