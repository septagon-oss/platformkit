package entity

import "strings"

// FormsHints opts individual forms into section steps. Nil members keep flat forms.
type FormsHints struct {
	Create *FormHints
	Update *FormHints
	Input  *FormHints
}

// FormHints declares a form layout. Empty Layout keeps the flat form; "steps"
// requires exactly one copy declaration per nonempty editable section.
type FormHints struct {
	Layout           string
	UnsectionedLabel string
	Steps            []FormStepHints
}

// FormStepHints supplies one step's action. Section "" names unsectioned fields.
// A single {answer} token requires AnswerField on this step and EmptyLabel;
// literal labels require neither. The last step also requires its own label.
type FormStepHints struct {
	Section      string
	PrimaryLabel string
	AnswerField  string
	EmptyLabel   string
}

// WithLabel answers with a product's translation of this step's label when it carries what
// the declaration declares — words a person could read, the same {answer} tokens, and no
// brace beyond them. Membership resolves from copy, so copy that fails costs this label.
func (h FormStepHints) WithLabel(said string) string {
	if strings.TrimSpace(said) != "" && strings.Count(said, "{answer}") == strings.Count(h.PrimaryLabel, "{answer}") && !strings.ContainsAny(strings.ReplaceAll(said, "{answer}", ""), "{}") {
		return said
	}
	return h.PrimaryLabel
}
