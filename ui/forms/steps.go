package forms

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	"github.com/septagon-oss/platformkit/ui/style"
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

// StepView supplies an already resolved position and encoded continuation.
// Navigation is a native POST address; the final action is Options.Action.
type StepView struct {
	Steps         []Step
	Current       int
	Continuation  string
	Navigation    string
	AnswerInvalid bool
}

// Step contains the fields and copy a caller resolved for one form screen.
type Step struct {
	Key, Label   string
	Fields       []string
	PrimaryLabel string
	AnswerField  string
	EmptyLabel   string
}

// StepExample renders one resolved step using the ordinary controls and Stepper.
// Callers validate position and continuation before rendering; no input is saved.
func StepExample(id string, model Model, options Options, step StepView) examples.Example {
	current := step.Steps[step.Current]
	word := func(key, fallback string) string {
		if options.Text != nil {
			return options.Text(key, fallback)
		}
		return fallback
	}
	progress := fmt.Sprintf(word("forms.step.progress", "Step %d of %d"), step.Current+1, len(step.Steps))
	steps := make([]components.Step, len(step.Steps))
	for i, s := range step.Steps {
		steps[i] = components.Step{Key: s.Key, Title: s.Label, State: "upcoming"}
		if i < step.Current {
			steps[i].State = "complete"
		}
	}
	slots := components.StepperSlots{Body: stepControls(model, options, current)}
	if model.Detail != "" || len(model.Errors) > 0 {
		slots.ErrorSummary = []g.Node{stepErrors(model, options, step, word("forms.step.correct", "Check your answers"))}
	}
	intent, action := "next", step.Navigation
	if step.Current == len(steps)-1 {
		intent, action = "submit", options.Action
	}
	label, labels := answerLabel(model, current)
	if step.AnswerInvalid {
		label = current.EmptyLabel
	}
	encoded, _ := json.Marshal(labels)
	primary := components.ButtonProps{Label: label, Type: "submit", ComponentProps: components.ComponentProps{Attrs: map[string]string{
		"name": "__pkit_intent", "value": intent, "formaction": action, "data-answer-field": current.AnswerField,
		"data-answer-template": current.PrimaryLabel, "data-answer-empty": current.EmptyLabel, "data-answer-labels": string(encoded),
	}}}
	slots.Continue = []g.Node{components.Button(primary)}
	if step.Current > 0 {
		slots.Back = []g.Node{components.Button(components.ButtonProps{Label: word("forms.step.back", "Back"), Type: "submit", Variant: "secondary",
			ComponentProps: components.ComponentProps{Attrs: map[string]string{"name": "__pkit_intent", "value": "back", "formaction": step.Navigation}}})}
	}
	slots.Cancel = []g.Node{components.Button(components.ButtonProps{Label: word("forms.step.cancel", "Cancel"), Variant: "link", Href: options.CancelURL})}
	props := components.FormProps{Action: options.Action, Label: options.Title, ComponentProps: components.ComponentProps{
		ID: options.Namespace + "-form", Class: style.New().Width(style.SFull).MaxWScaled(style.MaxWSM).Compile(),
		Attrs: map[string]string{"novalidate": "novalidate", "data-form-steps": "true", "hx-boost": "false"},
	}}
	return examples.ExampleWithChildren(examples.ExampleInfo{ID: id, ComponentID: "pk-ui.component.form", Group: "Screens", Name: options.Title}, props, []g.Node{
		h.Input(h.Type("hidden"), h.Name("__pkit_form"), h.Value(step.Continuation)),
		components.StepperWithSlots(components.StepperProps{Label: options.Title, Steps: steps, CurrentKey: current.Key, ProgressText: progress, Segmented: true}, slots),
	}, components.Form)
}

func stepErrors(model Model, options Options, step StepView, title string) g.Node {
	nodes := []g.Node{h.Role("alert"), g.Attr("tabindex", "-1"), g.Attr("data-step-errors", "true"), g.If(len(model.Errors) == 0, g.Attr("autofocus", "autofocus")), h.Strong(g.Text(title)), h.P(g.Text(model.Detail))}
	for i, group := range step.Steps {
		for _, name := range group.Fields {
			if message := model.Errors[name]; message != "" {
				label := name
				for _, field := range model.Fields {
					if field.Definition.Name == name {
						label = field.Label
					}
				}
				var text g.Node = g.Text(group.Label + ": " + label + " — " + message)
				if i == step.Current {
					text = h.A(h.Href("#"+options.Namespace+"-field-"+hex.EncodeToString([]byte(name))), text)
				}
				nodes = append(nodes, h.P(text))
			}
		}
	}
	return h.Div(nodes...)
}

func stepControls(model Model, options Options, step Step) []g.Node {
	body := []g.Node{h.H2(g.Attr("tabindex", "-1"), g.Attr("data-step-heading", "true"), g.Text(step.Label))}
	focused := false
	for _, name := range step.Fields {
		for _, field := range model.Fields {
			if field.Definition.Name != name {
				continue
			}
			err := model.Errors[name]
			focus := err != "" && !focused
			focused = focused || focus
			if err != "" && field.Definition.Widget != "richtext" && field.Definition.Type != entity.TypeBool {
				field.Definition.Type, field.Definition.Widget, field.Definition.Enum = entity.TypeString, "text", nil
			}
			field.Editor.Endpoint, field.Editor.DraftKey = "", ""
			body = append(body, Control(ControlProps{Field: field, ID: options.Namespace + "-field-" + hex.EncodeToString([]byte(name)),
				Value: model.Values[name], Error: err, AutoFocus: focus, Text: options.Text}))
		}
	}
	return body
}

func answerLabel(model Model, step Step) (string, map[string]string) {
	labels := map[string]string{}
	if step.AnswerField == "" {
		return step.PrimaryLabel, labels
	}
	value := strings.TrimSpace(model.Values[step.AnswerField])
	for _, f := range model.Fields {
		if f.Definition.Name == step.AnswerField {
			for _, option := range f.Options {
				labels[option.Value] = option.Label
			}
		}
	}
	if value == "" || model.Errors[step.AnswerField] != "" {
		return step.EmptyLabel, labels
	}
	if len(labels) > 0 {
		parts := strings.Split(value, ",")
		for i, part := range parts {
			label, ok := labels[strings.TrimSpace(part)]
			if !ok {
				return step.EmptyLabel, labels
			}
			parts[i] = label
		}
		value = strings.Join(parts, ", ")
	}
	return strings.Replace(step.PrimaryLabel, "{answer}", value, 1), labels
}
