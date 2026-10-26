package entity

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// ChoiceFigure is one field's compiled artwork, handed to the kernel by the owner
// that drew it. entity never parses markup: the figure arrives already compiled
// against the field's ordinary choice IDs, and that same owner keeps the decision
// which marks a selection, so a figure is a value the kernel can carry, compare and
// draw without ever trusting a client's bytes. A nil figure is no figure. The
// document form belongs to the owner: exactly a digest, the canonical SVG and the
// sorted choice IDs, and a reader that draws it independently recompiles that form.
type ChoiceFigure interface {
	// SVG returns the canonical drawing with nothing marked.
	SVG() string
	// ChoiceIDs returns the mapped ordinary IDs, byte-sorted and detached.
	ChoiceIDs() []string
	// Render returns the same drawing with each listed ID marked, refusing an ID
	// the figure does not map or a repeated one, and never changing its own bytes.
	Render(selected []string) (string, error)
}

// ChoiceFields overlays finite field metadata at composition, never in type caches.
type ChoiceFields struct{ Fields []ChoiceField }
type ChoiceField struct {
	Name    string
	Choices ChoiceSet
	Figure  ChoiceFigure
}

// ChoiceAppearance reports whether a hint names one of the four choice renderers.
func ChoiceAppearance(hint string) bool {
	return slices.Contains([]string{"tiles", "chips", "figure", "swatch"}, hint)
}

// WithChoices copies fields and applies all declarations atomically. Unknown names,
// duplicate targets and conflicting shapes/hints return no partial schema.
func WithChoices(fields []Field, declarations *ChoiceFields) ([]Field, error) {
	out := copyFields(fields)
	seen := map[string]bool{}
	if declarations != nil {
		for _, d := range declarations.Fields {
			at := slices.IndexFunc(out, func(f Field) bool { return f.Name == d.Name })
			if at < 0 || seen[d.Name] {
				return nil, fmt.Errorf("choice field %q: use a unique existing field name", d.Name)
			}
			seen[d.Name] = true
			out[at].Choices = d.Choices.Clone()
			if d.Figure != nil {
				out[at].Presentation.Figure = d.Figure
			}
		}
	}
	for i := range out {
		if err := ValidateChoices(out[i]); err != nil {
			return nil, fmt.Errorf("choice field %q: %w", out[i].Name, err)
		}
		if out[i].Choices != nil {
			out[i].Required = out[i].Choices.Empty.Mode == "refuse"
		}
	}
	return out, nil
}

// ValidateChoices checks the field's semantics, appearance and explicit default.
// Empty drafts remain renderable even when a new answer must be nonempty.
func ValidateChoices(f Field) error {
	if strings.HasPrefix(f.Name, "__pkit_choice_") {
		return choiceError("name", "The __pkit_choice_ prefix belongs to form transport.")
	}
	c, h := f.Choices, f.Presentation
	if c == nil {
		if ChoiceAppearance(f.Widget) || ChoiceAppearance(f.Present) || h.Figure != nil {
			return choiceError("choices", "Choice appearances require a choice declaration.")
		}
		return nil
	}
	if err := c.Validate(); err != nil {
		return err
	}
	if (c.Cardinality == "one" && f.Type != TypeString) || (c.Cardinality == "many" && (f.Type != TypeList || f.Elem != TypeString)) {
		return choiceError("cardinality", "One choice is a string; many choices are a string list.")
	}
	if f.Widget != "" && !ChoiceAppearance(f.Widget) {
		return choiceError("widget", "A choice is answered with tiles, chips, figure or swatch.")
	}
	if f.Present != "" {
		return choiceError("present", "A choice reads back as its selected options; its appearance names the control.")
	}
	if len(f.Enum)+len(h.EnumLabels)+len(h.EnumTones)+len(h.DoneValues) > 0 || h.Reference != nil || h.Money != nil || h.Format != "" {
		return choiceError("choices", "Choices cannot also declare enum, reference, money, format or status semantics.")
	}
	if f.Required && c.Empty.Mode == "allow" {
		return choiceError("empty", "Required conflicts with an allowed empty answer.")
	}
	if err := choiceArtwork(f); err != nil {
		return err
	}
	if f.Default != "" {
		value, err := DefaultValue(f)
		if err != nil {
			return err
		}
		if raw, ok := value.(json.RawMessage); ok {
			if err := json.Unmarshal(raw, &value); err != nil {
				return err
			}
		}
		ids, err := c.ChoiceIDs(value)
		if err != nil {
			return err
		}
		canonical, err := c.Answer(ids)
		if err != nil {
			return err
		}
		if !slices.Equal(ids, canonical) {
			return choiceError("default", "A default must already fit the maximum.")
		}
	}
	return nil
}

func choiceArtwork(f Field) error {
	c, figure := f.Choices, f.Presentation.Figure
	ids := []string{}
	for _, o := range c.Options {
		if o.ID == c.NoPreference {
			continue
		}
		ids = append(ids, o.ID)
		if f.Widget == "swatch" && (o.Picture == nil || o.Picture.Kind != "colour") {
			return choiceError("picture", "Every ordinary swatch needs a colour picture.")
		}
	}
	if f.Widget == "figure" || f.Present == "figure" {
		slices.Sort(ids)
		if figure == nil || figure.SVG() == "" || !slices.Equal(ids, figure.ChoiceIDs()) {
			return choiceError("figure", "Compile a figure mapping every ordinary choice.")
		}
	} else if figure != nil {
		return choiceError("figure", "A figure requires a figure appearance.")
	}
	return nil
}
