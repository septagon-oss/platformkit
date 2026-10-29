package present

import (
	"cmp"
	"slices"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/display"
)

func fields(in Input) []ResolvedField {
	out := make([]ResolvedField, 0, len(in.Schema.Fields))
	done := in.Hints.StatusField
	for _, f := range in.Schema.Fields {
		h := f.Presentation
		create := editableCreate(in, f)
		rf := ResolvedField{
			Name: f.Name, Label: display.FieldLabel(f), Help: display.FieldHelp(f),
			Section: h.Section, OnList: f.OnList(), OnRecord: visible(f),
			Editable: ResolvedEditable{Create: create, Update: editableUpdate(in, f)},
			Control:  control(f), Kind: kind(f),
			Reading: reading(in, f), Options: options(f, f.Name == done),
		}
		if h.Reference != nil {
			rf.Reference = &ResolvedReference{Resource: h.Reference.Resource}
		}
		resolveChoice(f, &rf)
		out = append(out, rf)
	}
	return out
}

// ControlKind is the effective form widget for a declared field. Unknown
// declarations are refused by the schema gate; scalar/list fallbacks use text.
func ControlKind(f entity.Field) string {
	if f.Widget == "" && (f.Type == entity.TypeText || f.Type == entity.TypeUUID) && len(f.Enum) == 0 {
		return "text"
	}
	return cmp.Or(control(f), "text")
}

func control(f entity.Field) string {
	if f.Widget != "" {
		return f.Widget
	}
	switch {
	case f.Type == entity.TypeSketch:
		return "sketch"
	case f.Choices != nil:
		return "chips"
	case len(f.Enum) > 0:
		return "select"
	case f.Type == entity.TypeText:
		return "textarea"
	case f.Type == entity.TypeInt || f.Type == entity.TypeFloat:
		return "number"
	case f.Type == entity.TypeBool:
		return "checkbox"
	case f.Type == entity.TypeTime:
		return "datetime"
	case f.Type == entity.TypeUUID && f.Presentation.Reference != nil:
		return "entity-picker"
	case f.Type == entity.TypeString || f.Type == entity.TypeUUID:
		return "text"
	}
	return ""
}

var kinds = map[entity.FieldType]string{
	entity.TypeString: "text", entity.TypeText: "longtext", entity.TypeInt: "number",
	entity.TypeFloat: "number", entity.TypeBool: "boolean", entity.TypeTime: "instant",
	entity.TypeSketch: "sketch", entity.TypeUUID: "text", entity.TypeList: "list", entity.TypeMap: "map", entity.TypePicture: "picture",
}

// kind is how a read draws the field: the declared presentation, else the word
// for a choice's selection, else the declared format, else the word for the type.
func kind(f entity.Field) string {
	h := f.Presentation
	switch {
	case f.Present != "":
		return f.Present
	case f.Choices != nil:
		return "choice"
	case h.Format != "" && entity.ValidFormat(h.Format):
		return h.Format
	case h.Money != nil:
		return "money"
	case len(f.Enum) > 0:
		return "enum"
	case f.Type == entity.TypeUUID && h.Reference != nil:
		return "reference"
	}
	return kinds[f.Type]
}

func reading(in Input, f entity.Field) ResolvedReading {
	h := f.Presentation
	format := h.Format
	if !entity.ValidFormat(format) {
		format = ""
	}
	if format == "" && f.Type == entity.TypeTime {
		format = "datetime"
	}
	if format == "" && h.Money != nil {
		format = "money"
	}
	r := ResolvedReading{Format: format, Missing: in.word("present.missing", "—")}
	if f.Type == entity.TypeBool {
		r.True, r.False = in.word("present.true", "Yes"), in.word("present.false", "No")
	}
	if f.Type == entity.TypeInt {
		zero := 0
		r.Precision = &zero
	}
	if f.Type == entity.TypeTime {
		r.Time = &ResolvedTime{Kind: "instant", Relative: format == "relative"}
		if format == "date" {
			r.Time.Kind = "date"
		}
	}
	if h.Money != nil {
		r.Money = &ResolvedMoney{CurrencyField: h.Money.CurrencyField, Scale: h.Money.Scale}
	}
	return r
}

// options are an enum's values in declared order; a value nobody labelled reads
// as Humanize(value) and one nobody toned is neutral. [] for a non-enum.
func options(f entity.Field, status bool) []ResolvedOption {
	out := make([]ResolvedOption, 0, len(f.Enum))
	for _, v := range f.Enum {
		tone := f.Presentation.EnumTones[v]
		if !entity.ValidTone(tone) {
			tone = "neutral"
		}
		out = append(out, ResolvedOption{
			Value: v, Label: display.EnumWord(f, v), Tone: tone,
			Done: status && slices.Contains(f.Presentation.DoneValues, v),
		})
	}
	return out
}
