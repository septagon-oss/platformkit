package components

import (
	"fmt"
	"strconv"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

type Option struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Disabled bool   `json:"disabled,omitzero"`
	Reason   string `json:"reason,omitempty"`
}

type OptionChipsProps struct {
	ComponentProps
	Label     string   `json:"label"`
	Name      string   `json:"name"`
	FormID    string   `json:"formID,omitempty"`
	Options   []Option `json:"options"`
	Value     string   `json:"value,omitempty"`
	Required  bool     `json:"required,omitzero"`
	ErrorText string   `json:"errorText,omitempty"`
}

func (p OptionChipsProps) Validate() error {
	if !required(p.ID, p.Label, p.Name) || len(p.Options) == 0 {
		return fmt.Errorf("OptionChips: identity, name, label and options required")
	}
	ids, selected := map[string]bool{}, p.Value == ""
	for _, option := range p.Options {
		if !required(option.Key, option.Label) || ids[option.Key] {
			return fmt.Errorf("OptionChips: unique keys and labels required")
		}
		ids[option.Key] = true
		if option.Key == p.Value && !option.Disabled {
			selected = true
		}
	}
	if !selected && !required(p.ErrorText) {
		return fmt.Errorf("OptionChips: stale choice requires ErrorText")
	}
	return nil
}
func OptionChips(p OptionChipsProps) g.Node {
	if err := p.Validate(); err != nil {
		return invalidComponent(err)
	}
	var choices []g.Node
	for _, option := range p.Options {
		attrs := []g.Node{h.Type("radio"), h.Name(p.Name), h.Value(option.Key), g.If(p.FormID != "", g.Attr("form", p.FormID)), g.If(p.Required, h.Required()), g.If(p.Disabled || option.Disabled, h.Disabled()), g.If(p.Value == option.Key && !option.Disabled, h.Checked())}
		if p.ErrorText != "" {
			attrs = append(attrs, g.Attr("aria-invalid", "true"), g.Attr("aria-describedby", p.ID+"-error"))
		}
		label := option.Label
		if option.Reason != "" {
			label += " · " + option.Reason
		}
		choices = append(choices, h.Label(h.Class(clChoice.Compile()), h.Input(attrs...), g.Text(label)))
	}
	return h.FieldSet(append(baseAttrs(p.ComponentProps), g.Attr("data-component", "option-chips"), g.Attr("data-shared-content", ""), h.Legend(h.Class(clLabel.Compile()), g.Text(p.Label)),
		Flex(FlexProps{Wrap: true, Gap: "2"}, choices...), g.If(p.ErrorText != "", h.P(h.ID(p.ID+"-error"), h.Class(clFieldErr.Compile()), g.Text(p.ErrorText))))...)
}

type QuantityInputProps struct {
	ComponentProps
	Label          string `json:"label"`
	Name           string `json:"name"`
	FormID         string `json:"formID,omitempty"`
	Value          int64  `json:"value,string"`
	Min            int64  `json:"min,string"`
	Max            int64  `json:"max,string"`
	Step           int64  `json:"step,string"`
	DecreaseLabel  string `json:"decreaseLabel"`
	IncreaseLabel  string `json:"increaseLabel"`
	ErrorText      string `json:"errorText,omitempty"`
	ValidationText string `json:"validationText"`
	Pending        bool   `json:"pending,omitzero"`
}

// ValidQuantity checks the caller's bounded step lattice without clamping input.
func ValidQuantity(value, min, max, step int64) bool {
	return min >= 0 && max >= min && step > 0 && value >= min && value <= max && (value-min)%step == 0
}
func (p QuantityInputProps) Validate() error {
	if !required(p.ID, p.Label, p.Name, p.DecreaseLabel, p.IncreaseLabel, p.ValidationText) || p.Min < 0 || p.Max < p.Min || p.Step <= 0 {
		return fmt.Errorf("QuantityInput: labels and a nonnegative bounded step range required")
	}
	if !ValidQuantity(p.Value, p.Min, p.Max, p.Step) && !required(p.ErrorText) {
		return fmt.Errorf("QuantityInput: invalid value needs the caller's ErrorText")
	}
	return nil
}
func QuantityInput(p QuantityInputProps) g.Node {
	if err := p.Validate(); err != nil {
		return invalidComponent(err)
	}
	attrs := map[string]string{"inputmode": "numeric", "data-quantity-value": "", "data-quantity-invalid": p.ValidationText, "data-min": strconv.FormatInt(p.Min, 10), "data-max": strconv.FormatInt(p.Max, 10), "data-step": strconv.FormatInt(p.Step, 10)}
	if p.FormID != "" {
		attrs["form"] = p.FormID
	}
	input := Input(InputProps{ComponentProps: ComponentProps{ID: p.ID + "-value", Disabled: p.Disabled || p.Pending, Attrs: attrs}, Label: p.Label, Name: p.Name, Value: strconv.FormatInt(p.Value, 10), Type: "text", Pattern: "[0-9]+", Required: true, Size: "lg", Error: p.ErrorText})
	control := func(label, direction string) g.Node {
		return h.Span(h.Hidden(""), g.Attr("data-quantity-enhancement", ""), recoveryAction(ButtonProps{ComponentProps: ComponentProps{Attrs: map[string]string{"data-quantity-direction": direction}}, Label: label, Variant: "outline"}, p.Disabled || p.Pending))
	}
	return h.Div(append(baseAttrs(p.ComponentProps), g.Attr("data-component", "quantity-input"), g.Attr("data-shared-content", ""), g.Attr("aria-busy", boolText(p.Pending)),
		Flex(FlexProps{Wrap: true, Gap: "2", Align: "end"}, control(p.DecreaseLabel, "-1"), input, control(p.IncreaseLabel, "1")))...)
}
