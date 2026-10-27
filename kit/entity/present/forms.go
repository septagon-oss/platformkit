package present

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/septagon-oss/platformkit/kit/entity"
)

// ResolvedForms contains only opted-in forms; absent members keep flat rendering.
//
// Every resolved component closes its members, so a strict reader names the component
// an object answers from the members it holds. "create" and "update" are already the
// whole object of a field's editability and the names of a layout's two field lists,
// so an object holding them would answer three components at once and no reader could
// correct the reading; these members carry the Form suffix that says which one they are.
type ResolvedForms struct {
	Create *ResolvedForm `json:"createForm,omitempty"`
	Update *ResolvedForm `json:"updateForm,omitempty"`
	Input  *ResolvedForm `json:"inputForm,omitempty"`
}

// ResolvedForm is ordered editable membership and copy, without answers or state.
type ResolvedForm struct {
	Layout string         `json:"layout"`
	Steps  []ResolvedStep `json:"steps"`
}

// ResolvedStep names one form screen. Fields retain schema order within a section.
type ResolvedStep struct {
	Key          string   `json:"key"`
	Section      string   `json:"section"`
	Label        string   `json:"label"`
	Fields       []string `json:"fields"`
	PrimaryLabel string   `json:"primaryLabel"`
	AnswerField  string   `json:"answerField"`
	EmptyLabel   string   `json:"emptyLabel"`
}

func forms(in Input, l ResolvedLayout) *ResolvedForms {
	h := in.Hints.Forms
	if h == nil {
		return nil
	}
	label := in.word("present.form.details", "Details")
	create, _ := ResolveForm(h.Create, in.Hints.Sections, in.Schema.Fields, l.Create, label)
	update, _ := ResolveForm(h.Update, in.Hints.Sections, in.Schema.Fields, l.Update, label)
	input, _ := ResolveForm(h.Input, in.Hints.Sections, in.Schema.Fields, form(in, "input", func(f entity.Field) bool { return editableCreate(in, f) }), label)
	if create == nil && update == nil && input == nil {
		return nil
	}
	return &ResolvedForms{Create: create, Update: update, Input: input}
}

// ResolveForm checks and resolves a declaration against editable names. It returns
// nil for a flat form, or an error for contradictory copy, membership or controls.
// All returned slices are detached; translated copy never changes membership.
func ResolveForm(h *entity.FormHints, sections []entity.EntitySection, fields []entity.Field, editable []string, fallback string) (*ResolvedForm, error) {
	if h == nil {
		return nil, nil
	}
	if h.Layout == "" && len(h.Steps) == 0 && h.UnsectionedLabel == "" {
		return nil, nil
	}
	if h.Layout != "steps" {
		return nil, fmt.Errorf("form layout must be steps, or flat without step properties")
	}
	groups := []entity.EntitySection{{Label: cmp.Or(h.UnsectionedLabel, fallback)}}
	seen := map[string]bool{"": true}
	for _, section := range sections {
		if seen[section.Key] || section.Label == "" {
			return nil, fmt.Errorf("step sections require unique nonempty keys and labels")
		}
		seen[section.Key] = true
		groups = append(groups, section)
	}
	members := map[string][]string{}
	for _, f := range fields {
		if !slices.Contains(editable, f.Name) {
			continue
		}
		if !seen[f.Presentation.Section] || strings.HasPrefix(f.Name, "__pkit_") || f.Name == entity.RevisionCondition {
			return nil, fmt.Errorf("field %q has an unknown section or reserved name", f.Name)
		}
		if f.Widget == "file" || f.Widget == "password" {
			return nil, fmt.Errorf("field %q cannot travel in a form continuation", f.Name)
		}
		members[f.Presentation.Section] = append(members[f.Presentation.Section], f.Name)
	}
	if h.UnsectionedLabel != "" && len(members[""]) == 0 {
		return nil, fmt.Errorf("unsectioned label has no editable fields")
	}
	copy := map[string]entity.FormStepHints{}
	for _, c := range h.Steps {
		if _, exists := copy[c.Section]; exists || len(members[c.Section]) == 0 {
			return nil, fmt.Errorf("step copy for %q is duplicated or unused", c.Section)
		}
		copy[c.Section] = c
	}
	out := &ResolvedForm{Layout: "steps", Steps: []ResolvedStep{}}
	for _, section := range groups {
		names := members[section.Key]
		if len(names) == 0 {
			continue
		}
		c := copy[section.Key]
		if err := stepCopy(c, names); err != nil {
			return nil, fmt.Errorf("step %q: %w", section.Key, err)
		}
		key := "section:" + section.Key
		if section.Key == "" {
			key = "unsectioned"
		}
		out.Steps = append(out.Steps, ResolvedStep{key, section.Key, section.Label, names, c.PrimaryLabel, c.AnswerField, c.EmptyLabel})
	}
	if len(out.Steps) == 0 {
		return nil, fmt.Errorf("steps require editable fields")
	}
	return out, nil
}

func stepCopy(c entity.FormStepHints, names []string) error {
	count := strings.Count(c.PrimaryLabel, "{answer}")
	literal := strings.ReplaceAll(c.PrimaryLabel, "{answer}", "")
	if strings.TrimSpace(c.PrimaryLabel) == "" || strings.ContainsAny(literal, "{}") || count > 1 {
		return fmt.Errorf("primary label requires literal text or one {answer} token")
	}
	if count == 1 && (!slices.Contains(names, c.AnswerField) || strings.TrimSpace(c.EmptyLabel) == "") {
		return fmt.Errorf("answer label requires a field on this step and an empty label")
	}
	if count == 0 && (c.AnswerField != "" || c.EmptyLabel != "") {
		return fmt.Errorf("literal label cannot declare answer properties")
	}
	return nil
}
