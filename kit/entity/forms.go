package entity

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
