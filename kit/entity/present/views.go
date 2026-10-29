package present

import (
	"slices"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/screen"
)

// ResolvedControl is one visible control, including its source path and current
// editability. Hidden transport fields are not controls.
type ResolvedControl struct {
	Name         string               `json:"name"`
	Path         []string             `json:"path"`
	Control      string               `json:"control"`
	Editable     bool                 `json:"editable"`
	Interactions []screen.Interaction `json:"interactions"`
}

// ResolvedView is the control decision shared by publication and rendering.
// A repeated command retains its view but has no uniquely targetable controls.
type ResolvedView struct {
	Kind        string             `json:"kind"`
	Verb        string             `json:"verb"`
	Cardinality screen.Cardinality `json:"cardinality"`
	Controls    []ResolvedControl  `json:"controls"`
}

// views are the control decisions of the screens this entry offers. A form view
// names the controls in the order the form draws them — the resolved layout, then
// the columns it shows read-only — so the published view and the drawn page are
// one decision rather than two that have to agree.
func views(in Input, l ResolvedLayout, resolved []ResolvedField) []ResolvedView {
	out := []ResolvedView{}
	if in.CommandsOnly {
		return out
	}
	add := func(kind string, controls []ResolvedControl) {
		out = append(out, ResolvedView{Kind: kind, Cardinality: screen.One, Controls: controls})
	}
	if in.offers("list") {
		controls := []ResolvedControl{}
		if in.offers("create") {
			controls = append(controls, action("create", false))
		}
		add("list", controls)
	}
	if in.offers("read") {
		controls := []ResolvedControl{}
		if in.offers("update") {
			controls = append(controls, action("edit", false))
		}
		if in.offers("delete") {
			controls = append(controls, action("delete", true))
		}
		add("detail", controls)
	}
	if in.offers("create") {
		add("create", formControls(in, l.Create, nil))
	}
	if in.offers("update") {
		add("edit", formControls(in, l.Update, readOnlyFormFields(in, l.Update, resolved)))
	}
	for _, c := range in.Commands {
		if c.Hints.System || c.Collection && !in.offers("list") || !c.Collection && !in.offers("read") || in.Singleton {
			continue
		}
		v := CommandView(c)
		out = append(out, v)
	}
	return out
}

func action(name string, submission bool) ResolvedControl {
	c := ResolvedControl{Name: "action." + name, Path: []string{}, Control: "link", Interactions: []screen.Interaction{screen.Activated}}
	if submission {
		c.Control = "button"
		c.Interactions = append(c.Interactions, screen.Submitted)
	}
	return c
}

// formControls are the form's controls in drawing order: the layout's asked-for
// fields, then the columns the update form carries but keeps still, then the two
// buttons. A create form asks for nothing it will not take.
func formControls(in Input, names []string, readOnly []entity.Field) []ResolvedControl {
	out := make([]ResolvedControl, 0, len(names)+2)
	for _, name := range names {
		f, ok := entity.FieldNamed(in.Schema.Fields, name)
		if !ok || f.Widget == "hidden" {
			continue
		}
		out = append(out, fieldControls(f, true)...)
	}
	for _, f := range readOnly {
		out = append(out, fieldControls(f, false)...)
	}
	return append(out, action("submit", true), action("cancel", false))
}

// readOnlyFormFields are the columns an update form draws without letting this
// write change them: a field a command owns, shown so the row reads whole.
func readOnlyFormFields(in Input, names []string, resolved []ResolvedField) []entity.Field {
	out := []entity.Field{}
	for _, p := range resolved {
		if !p.OnRecord || p.Editable.Update || slices.Contains(names, p.Name) {
			continue
		}
		f, ok := entity.FieldNamed(in.Schema.Fields, p.Name)
		if !ok || f.Widget == "hidden" || entity.ServerOwned(f) {
			continue
		}
		out = append(out, f)
	}
	return out
}

func fieldControls(f entity.Field, editable bool) []ResolvedControl {
	if len(f.Keys) != 0 {
		out := []ResolvedControl{}
		for _, key := range f.Keys {
			if key.From == entity.KeyFromRevision || key.Widget == "hidden" {
				continue
			}
			c := fieldControl(entity.Field{Name: key.Name, Type: f.Elem, Widget: key.Widget}, editable)
			c.Path = []string{f.Name, key.Name}
			out = append(out, c)
		}
		return out
	}
	return []ResolvedControl{fieldControl(f, editable)}
}

func fieldControl(f entity.Field, editable bool) ResolvedControl {
	interactions := []screen.Interaction{}
	if editable {
		interactions = append(interactions, screen.Committed)
	}
	return ResolvedControl{Name: "field", Path: []string{f.Name}, Control: ControlKind(f), Editable: editable, Interactions: interactions}
}

// CommandView resolves one command's visible fields and submit control. Repeated
// language/history forms publish no unique anchors, even with one current row.
func CommandView(c CommandInput) ResolvedView {
	v := ResolvedView{Kind: "command", Verb: c.Verb, Cardinality: screen.One, Controls: []ResolvedControl{}}
	if c.Repeated || RepeatedFields(c.Fields) {
		v.Cardinality = screen.Many
		return v
	}
	for _, f := range c.Fields {
		if c.Bindings[f.Name] != "" || f.Widget == "hidden" {
			continue
		}
		v.Controls = append(v.Controls, fieldControls(f, true)...)
	}
	v.Controls = append(v.Controls, ResolvedControl{Name: "command", Path: []string{c.Verb}, Control: "button", Interactions: []screen.Interaction{screen.Activated, screen.Submitted}})
	return v
}

// RepeatedFields reports revision-map arguments whose forms repeat per locale.
func RepeatedFields(fields []entity.Field) bool {
	for _, f := range fields {
		for _, k := range f.Keys {
			if k.From == entity.KeyFromRevision {
				return true
			}
		}
	}
	return false
}
