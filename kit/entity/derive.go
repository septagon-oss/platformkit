package entity

import (
	"reflect"
	"strconv"
	"strings"
)

func deriveField(t reflect.Type, sf reflect.StructField) (Field, bool) {
	if sf.Anonymous || !sf.IsExported() {
		return Field{}, false
	}
	name, ok := jsonName(sf)
	if !ok {
		return Field{}, false
	}
	kind, elem, ok := fieldType(sf)
	if !ok {
		return Field{}, false
	}
	f := Field{
		Name:     name,
		Column:   column(sf),
		Type:     kind,
		Elem:     elem,
		Required: has(sf.Tag.Get("validate"), "required"),
		ReadOnly: declaredBy(t, sf) == baseType || (kind == TypePicture && sf.Tag.Get("readOnly") == "true"),
		Default:  sf.Tag.Get("default"),
		Doc:      sf.Tag.Get("doc"),
		Index:    sf.Index,
	}
	f.MaxLength, _ = strconv.Atoi(sf.Tag.Get("maxLength"))
	// Translation is data metadata, independent of the selected UI widget.
	f.Translatable = has(sf.Tag.Get("i18n"), "translatable")
	if enum := sf.Tag.Get("enum"); enum != "" {
		f.Enum = strings.Split(enum, ",")
	}
	return f, true
}

func readUI(f *Field, raw string) {
	// Both comma and semicolon separate UI directives in existing entities.
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' }) {
		switch key, value, _ := strings.Cut(part, ":"); key {
		case "widget":
			f.Widget = value
		case "present":
			f.Present = value
		case "hide":
			f.HideList = f.HideList || value == "list"
		case "display":
			// No value is read: `ui:"display:yes"` sets the same flag as `ui:"display"`, and a value
			// would be a second shape nothing renders.
			f.Display = true
		case "label":
			f.Presentation.Label = value
		case "help":
			f.Presentation.Help = value
		case "section":
			f.Presentation.Section = value
		case "visibility":
			// A bare `visibility:` sets the empty string, which is "not declared"
			// and so refuses at mount rather than silently meaning "shown".
			f.Presentation.Visibility = value
		case "format":
			f.Presentation.Format = value
		case "reference":
			f.Presentation.Reference = &FieldReference{Resource: value}
		case "currency", "scale":
			// Currency and scale form one wire object; the mount gate refuses
			// an incomplete pair.
			if f.Presentation.Money == nil {
				f.Presentation.Money = &FieldMoney{}
			}
			if key == "currency" {
				f.Presentation.Money.CurrencyField = value
			} else {
				f.Presentation.Money.Scale, _ = strconv.Atoi(value)
			}
		}
	}
}

func readEnumPresentation(f *Field, tags reflect.StructTag) {
	// Use a separate tag for comma-separated values because commas also
	// separate UI directives.
	if raw := tags.Get("doneValues"); raw != "" {
		f.Presentation.DoneValues = strings.Split(raw, ",")
	}
	for _, tag := range []struct {
		name string
		into *map[string]string
	}{
		{"enumLabels", &f.Presentation.EnumLabels},
		{"enumTones", &f.Presentation.EnumTones},
	} {
		// Separate tags distinguish presentation maps from the enum value set.
		// Map values cannot contain commas; the mount gate enforces that limit.
		if raw := tags.Get(tag.name); raw != "" {
			into := make(map[string]string)
			for _, pair := range strings.Split(raw, ",") {
				key, value, _ := strings.Cut(pair, "=")
				into[key] = value
			}
			*tag.into = into
		}
	}
}
