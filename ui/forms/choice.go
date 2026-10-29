package forms

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/kit/screen"
	"github.com/septagon-oss/platformkit/ui/components"
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

// hint joins the field's own help line with the lines this surface has to say
// about the control it chose. The choice surface draws its own help paragraph
// rather than a component's, so the joining happens here; every other control
// hands its notes to ui/components, which draws one sentence per author.
func hint(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, " ")
}

func validateChoiceField(f Field) error {
	if err := entity.ValidateChoices(f.Definition); err != nil {
		return err
	}
	if f.Definition.Choices != nil && (f.Resolved == nil || f.Resolved.Choice == nil || f.Resolved.Name != f.Definition.Name || !entity.ChoiceAppearance(f.Resolved.Control)) {
		return fmt.Errorf("forms: choice %q requires its resolved policy", f.Definition.Name)
	}
	return nil
}

func choiceModelValues(m Model, f entity.Field) []string {
	if ids, ok := m.ChoiceValues[f.Name]; ok {
		return ids
	}
	if f.Choices != nil && m.Create && f.Default != "" {
		value, err := entity.DefaultValue(f)
		if err == nil {
			if raw, ok := value.(json.RawMessage); ok {
				_ = json.Unmarshal(raw, &value)
			}
			ids, _ := f.Choices.ChoiceIDs(value)
			return ids
		}
	}
	return nil
}

// choiceControl draws one choice's editing surface. anchor names the composed
// screen's own control for this field; empty leaves a standalone form unmarked.
func choiceControl(p ControlProps, anchor screen.AnchorRef) g.Node {
	if err := validateChoiceField(p.Field); err != nil {
		return g.NodeFunc(func(io.Writer) error { return err })
	}
	r := *p.Field.Resolved
	if p.Immutable {
		return ChoiceRead(r, p.ChoiceValues, p.Field.PictureSource, p.Field.ComposedSource, p.Text)
	}
	p.ID = cmp.Or(p.ID, "choice-"+p.Field.Definition.Name)
	c := r.Choice
	config, _ := json.Marshal(c)
	order, _ := json.Marshal(append([]string{}, p.ChoiceValues...))
	word := p.Text
	if word == nil {
		word = func(_, fallback string) string { return fallback }
	}
	help := r.Help
	if c.Maximum != nil && c.Maximum.Behaviour == "replace-oldest" {
		help = hint(help, word("forms.choice.order", "New choices in this form are applied in list order; the oldest are replaced."))
	}
	if c.NoPreference != "" {
		help = hint(help, word("forms.choice.exclusive", "No preference clears the other choices on submit. Uncheck it to choose others."))
	}
	words, _ := json.Marshal(map[string]string{"selected": word("forms.choice.selected", "selected"), "removed": word("forms.choice.removed", "Removed"), "matches": word("forms.choice.matches", "matching choices"), "noResults": word("forms.choice.noResults", "No matching choices")})
	stale := slices.ContainsFunc(p.ChoiceValues, func(id string) bool {
		return !slices.ContainsFunc(r.Options, func(o present.ResolvedOption) bool { return o.Value == id })
	})
	errorText := p.Error
	if _, err := p.Field.Definition.Choices.Change(nil, p.ChoiceValues); err != nil && errorText == "" {
		errorText = err.Error()
	}
	content := []g.Node{g.Attr("data-choice-surface", r.Control), g.Attr("data-choice-words", string(words)), g.Attr("data-choice-stale", strconv.FormatBool(stale)), g.Attr("data-choice-config", string(config)), g.Attr("data-choice-initial", string(order)), h.ID(p.ID), h.Legend(g.Text(r.Label)),
		choiceSummary(r, p.ChoiceValues, p.Field.PictureSource, p.Field.ComposedSource, word),
		h.P(h.ID(p.ID+"-help"), g.Text(help)), h.P(h.ID(p.ID+"-error"), g.Text(errorText)), h.P(g.Attr("data-choice-status", ""), g.Attr("role", "status")),
		h.Input(h.Type("hidden"), h.Name("__pkit_choice_present["+r.Name+"]"), h.Value("1")),
	}
	for _, option := range r.Options {
		if picture := choicePicture(option.Picture, p.Field.PictureSource, p.Field.ComposedSource); picture != nil {
			content = append(content, h.Template(g.Attr("data-choice-picture", option.Value), picture))
		}
	}
	if c.Cardinality == "many" {
		content = append(content, h.Input(h.Type("hidden"), h.Name("__pkit_choice_order["+r.Name+"]"), h.Value(choiceOrderValue(r, p.ChoiceValues)), g.Attr("data-choice-order", "")))
	}
	if c.SearchEnabled {
		content = append(content, h.Label(h.Hidden(""), g.Attr("data-choice-search-label", ""), g.Text(word("forms.choice.search", "Search choices")), h.Input(h.Type("search"), g.Attr("data-choice-search", ""))), h.NoScript(h.P(g.Text(word("forms.choice.find", "Use your browser’s find function to find a choice.")))))
	}
	chips := choiceItems(p, errorText, word)
	if r.Control == "figure" {
		content = append(content, h.Div(g.Attr("data-choice-layout", ""), choiceFigure(r, p.ChoiceValues), chips))
	} else {
		content = append(content, chips)
	}
	return h.FieldSet(append(content, anchorNodes(anchor)...)...)
}

func choiceItems(p ControlProps, errorText string, word func(string, string) string) g.Node {
	r := p.Field.Resolved
	opts := make([]components.Option, 0, len(r.Options))
	for _, o := range r.Options {
		opts = append(opts, components.Option{Key: o.Value, Label: o.Label})
	}
	for _, id := range p.ChoiceValues {
		if !slices.ContainsFunc(opts, func(o components.Option) bool { return o.Key == id }) {
			opts = append(opts, components.Option{Key: id, Label: id + " — " + word("forms.choice.unavailable", "Unavailable choice")})
		}
	}
	props := components.OptionChipsProps{ComponentProps: components.ComponentProps{ID: p.ID}, Name: r.Name, Label: r.Label, DescribedBy: p.ID + "-help " + p.ID + "-error", Options: opts, Values: p.ChoiceValues, Multiple: r.Choice.Cardinality == "many", Required: r.Choice.Empty.Mode == "refuse", ErrorText: errorText}
	if len(p.ChoiceValues) > 0 {
		props.Value = p.ChoiceValues[0]
	}
	if r.Choice.Cardinality == "one" && r.Choice.Empty.Mode == "allow" {
		props.ClearLabel = word("forms.choice.clear", "Clear selection")
	}
	pictures := func(o components.Option) g.Node {
		at := slices.IndexFunc(r.Options, func(option present.ResolvedOption) bool { return option.Value == o.Key })
		if at < 0 {
			return nil
		}
		option := r.Options[at]
		group := ""
		for _, g := range r.Choice.Groups {
			if g.Key == option.Group {
				group = g.Label
			}
		}
		return h.Span(g.Attr("data-choice-group", group), g.If(group != "", h.Small(g.Text(group))), g.If(r.Control == "tiles" || r.Control == "swatch", choicePicture(option.Picture, p.Field.PictureSource, p.Field.ComposedSource)))
	}
	return h.Div(g.Attr("data-choice-options", ""), components.OptionChipsItems(props, pictures))
}

func choiceFigure(r present.ResolvedField, ids []string) g.Node {
	if r.Figure == nil {
		return nil
	}
	highlighted := slices.DeleteFunc(slices.Clone(ids), func(id string) bool { return !slices.Contains(r.Figure.ChoiceIDs(), id) })
	svg, err := r.Figure.Render(highlighted)
	if err != nil {
		return g.Text(err.Error())
	}
	return h.Div(g.Attr("data-choice-figure", ""), g.Attr("aria-hidden", "true"), g.Raw(svg))
}

func choicePicture(p *entity.ChoicePicture, files, composed string) g.Node {
	if p == nil || p.Validate() != nil {
		return nil
	}
	if p.Kind == "colour" {
		return h.Span(g.Attr("data-choice-colour", p.Value), g.Attr("aria-hidden", "true"), g.El("svg", g.Attr("viewBox", "0 0 40 40"), g.Attr("focusable", "false"), g.El("circle", g.Attr("cx", "20"), g.Attr("cy", "20"), g.Attr("r", "18"), g.Attr("fill", p.Value))))
	}
	src := ""
	if p.Kind == "file" && files != "" {
		src = strings.ReplaceAll(files, "{id}", p.Value)
	}
	if p.Kind == "composed" && composed != "" {
		src = strings.ReplaceAll(composed, "{reference}", p.Value)
	}
	if src == "" {
		return nil
	}
	return components.Media(components.MediaProps{Src: src, Decorative: true, Horizontal: true, Lazy: true, Fit: "cover"})
}

func choiceSummary(r present.ResolvedField, ids []string, files, composed string, word func(string, string) string) g.Node {
	pictures := []g.Node{}
	labels := []string{}
	for _, id := range ids {
		at := slices.IndexFunc(r.Options, func(o present.ResolvedOption) bool { return o.Value == id })
		if at < 0 {
			labels = append(labels, id+" — "+word("forms.choice.unavailable", "Unavailable choice"))
			continue
		}
		o := r.Options[at]
		labels = append(labels, o.Label)
		if len(pictures) < 4 {
			if picture := choicePicture(o.Picture, files, composed); picture != nil {
				pictures = append(pictures, picture)
			}
		}
	}
	return h.Div(g.Attr("data-choice-summary", ""), h.Div(g.Attr("data-choice-stack", ""), g.Group(pictures)), h.Span(g.Attr("data-choice-count", ""), g.Text(strconv.Itoa(len(ids))+" "+word("forms.choice.selected", "selected"))), h.Span(g.Attr("data-choice-names", ""), g.Text(strings.Join(labels, ", "))), h.NoScript(h.Small(g.Text(word("forms.choice.rendered", "Last rendered selection")))))
}

// ChoiceRead renders typed selections without inputs, actions or transport markers.
// Unknown stored IDs remain visible for repair; nil text keeps English copy.
func ChoiceRead(r present.ResolvedField, ids []string, files, composed string, text func(string, string) string) g.Node {
	unavailable := "Unavailable choice"
	if text != nil {
		unavailable = text("forms.choice.unavailable", unavailable)
	}
	items := []g.Node{}
	for _, id := range ids {
		at := slices.IndexFunc(r.Options, func(o present.ResolvedOption) bool { return o.Value == id })
		label := id + " — " + unavailable
		var picture g.Node
		if at >= 0 {
			o := r.Options[at]
			label = o.Label
			if r.Kind == "tiles" || r.Kind == "swatch" {
				picture = choicePicture(o.Picture, files, composed)
			}
		}
		items = append(items, h.Span(g.Attr("data-choice-read-item", ""), picture, g.Text(label)))
	}
	if len(items) == 0 {
		items = append(items, g.Text(r.Reading.Missing))
	}
	return h.Div(g.Attr("data-choice-read", r.Kind), choiceFigure(r, ids), h.Div(g.Group(items)))
}

func choiceOrderValue(r present.ResolvedField, ids []string) string {
	known := slices.DeleteFunc(append([]string{}, ids...), func(id string) bool {
		return !slices.ContainsFunc(r.Options, func(o present.ResolvedOption) bool { return o.Value == id })
	})
	data, _ := json.Marshal(known)
	return string(data)
}
