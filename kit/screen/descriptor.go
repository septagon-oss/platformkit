package screen

import (
	"fmt"
	"slices"
	"strings"
)

// Cardinality is the number of instances a view can render on one page.
type Cardinality string

const (
	One  Cardinality = "one"
	Many Cardinality = "many"
)

// FixedInput is a declared form/query constant, never a record or caller value.
type FixedInput struct {
	Location  string   `json:"location"`
	FieldPath []string `json:"fieldPath"`
	Value     string   `json:"value"`
}

// BindingData is a wire value; constructors and artifact readers validate it.
type BindingData struct {
	Kind      string       `json:"kind"`
	Target    string       `json:"target"`
	FieldPath []string     `json:"fieldPath"`
	Fixed     []FixedInput `json:"fixed"`
}

// Binding is an immutable semantic destination. Its zero value is invalid.
type Binding struct{ data BindingData }

// Navigation binds activation to a destination view. It panics for an invalid
// declaration; untrusted wire input belongs in ParseSnapshot.
func Navigation(target ViewRef) Binding {
	return declaredBinding(BindingData{Kind: "navigation", Target: string(target)})
}

// Value binds a field path to the operation receiving it. Fixed inputs describe
// constants in the same form. All slices are copied; invalid declarations panic.
func Value(operation string, path []string, fixed ...FixedInput) Binding {
	return declaredBinding(BindingData{Kind: "value", Target: operation, FieldPath: path, Fixed: fixed})
}

// Submission binds a button to its operation and fixed form/query inputs.
// Invalid declarations panic; no handler or operation is executed.
func Submission(operation string, fixed ...FixedInput) Binding {
	return declaredBinding(BindingData{Kind: "submission", Target: operation, Fixed: fixed})
}

// Display names one read-only element's semantic source. Empty keys panic.
func Display(key string) Binding {
	return declaredBinding(BindingData{Kind: "display", Target: key})
}

func declaredBinding(data BindingData) Binding {
	if err := validateBinding(data); err != nil {
		panic(err)
	}
	return Binding{cloneBinding(data)}
}

func cloneBinding(b BindingData) BindingData {
	b.FieldPath = append([]string{}, b.FieldPath...)
	b.Fixed = append([]FixedInput{}, b.Fixed...)
	for i := range b.Fixed {
		b.Fixed[i].FieldPath = append([]string{}, b.Fixed[i].FieldPath...)
	}
	return b
}

func validPath(path []string) bool {
	if len(path) == 0 {
		return false
	}
	for _, part := range path {
		if _, err := Segment(part); err != nil {
			return false
		}
	}
	return true
}

func validateBinding(b BindingData) error {
	if _, err := Segment(b.Target); err != nil {
		return fmt.Errorf("screen: invalid binding target %q", b.Target)
	}
	switch b.Kind {
	case "navigation":
		if _, err := ParseView(b.Target); err != nil {
			return err
		}
	case "value":
		if !validPath(b.FieldPath) {
			return fmt.Errorf("screen: value binding needs a field path")
		}
	case "submission", "display":
	default:
		return fmt.Errorf("screen: unknown binding kind %q", b.Kind)
	}
	if b.Kind != "value" && len(b.FieldPath) != 0 {
		return fmt.Errorf("screen: only value bindings have field paths")
	}
	if (b.Kind == "navigation" || b.Kind == "display") && len(b.Fixed) != 0 {
		return fmt.Errorf("screen: %s bindings cannot carry fixed inputs", b.Kind)
	}
	seen := map[string]bool{}
	for _, f := range b.Fixed {
		key := f.Location + ":" + pathName(f.FieldPath)
		if (f.Location != "form" && f.Location != "query") || !validPath(f.FieldPath) || seen[key] {
			return fmt.Errorf("screen: invalid or duplicate fixed input %q", key)
		}
		seen[key] = true
	}
	return nil
}

func pathName(path []string) string {
	parts := make([]string, 0, len(path))
	for _, p := range path {
		s, _ := Segment(p)
		parts = append(parts, s)
	}
	return strings.Join(parts, ".")
}

// AnchorSpec is the input to a validated immutable anchor.
type AnchorSpec struct {
	Name         string
	Control      string
	Binding      Binding
	Interactions []Interaction
}

// Anchor is one unique control. Accessors return copies of retained slices.
type Anchor struct{ spec AnchorSpec }

// NewAnchor validates role, binding, control and supported interactions.
func NewAnchor(spec AnchorSpec) (Anchor, error) {
	p, err := anchorParts(spec.Name)
	if err != nil {
		return Anchor{}, err
	}
	b := spec.Binding.data
	if err := validateBinding(b); err != nil {
		return Anchor{}, err
	}
	if !validControl(spec.Control) || !bindingControl(b.Kind, spec.Control) {
		return Anchor{}, fmt.Errorf("screen: unknown control %q", spec.Control)
	}
	valid := p[0] == "field" && (b.Kind == "value" || b.Kind == "display") ||
		p[0] == "action" && (b.Kind == "navigation" || b.Kind == "submission") ||
		p[0] == "command" && b.Kind == "submission"
	if !valid {
		return Anchor{}, fmt.Errorf("screen: %s cannot use a %s binding", spec.Name, b.Kind)
	}
	seen := map[Interaction]bool{}
	for _, interaction := range spec.Interactions {
		allowed := interaction == Committed && b.Kind == "value" ||
			interaction == Activated && (b.Kind == "navigation" || b.Kind == "submission") ||
			interaction == Submitted && b.Kind == "submission"
		if !allowed || seen[interaction] {
			return Anchor{}, fmt.Errorf("screen: invalid or duplicate interaction %q on %s", interaction, spec.Name)
		}
		seen[interaction] = true
	}
	spec.Binding = Binding{cloneBinding(b)}
	spec.Interactions = append([]Interaction{}, spec.Interactions...)
	return Anchor{spec}, nil
}

func validControl(control string) bool {
	return slices.Contains(strings.Fields("link button display checkbox color date datetime email entity-picker file month number password richtext search select tel text textarea time url week"), control)
}

func (a Anchor) Name() string                { return a.spec.Name }
func (a Anchor) Control() string             { return a.spec.Control }
func (a Anchor) Binding() BindingData        { return cloneBinding(a.spec.Binding.data) }
func (a Anchor) Interactions() []Interaction { return slices.Clone(a.spec.Interactions) }

// Supports answers whether this descriptor admits an interaction. It does not
// assert that a request is authorized or that an operation succeeded.
func (a Anchor) Supports(interaction Interaction) bool {
	return slices.Contains(a.spec.Interactions, interaction)
}

// ViewSpec is the input to a validated immutable view. Hosts name the parent
// views of an inline command; ordinary views have no hosts.
type ViewSpec struct {
	Ref         ViewRef
	Cardinality Cardinality
	Hosts       []ViewRef
	Anchors     []AnchorSpec
}

// View is an immutable descriptor. Its zero value is invalid.
type View struct {
	ref         ViewRef
	cardinality Cardinality
	hosts       []ViewRef
	anchors     []Anchor
}

// NewView validates the whole descriptor and returns no partial value on error.
// Cardinality is explicit; repeated views cannot publish unique anchors.
func NewView(spec ViewSpec) (View, error) {
	if _, err := ParseView(string(spec.Ref)); err != nil {
		return View{}, err
	}
	if spec.Cardinality != One && spec.Cardinality != Many {
		return View{}, fmt.Errorf("screen: invalid cardinality %q", spec.Cardinality)
	}
	if spec.Cardinality == Many && len(spec.Anchors) != 0 {
		return View{}, fmt.Errorf("screen: repeated rows require a typed instance binding; %s cannot publish anchors", spec.Ref)
	}
	seenHosts := map[ViewRef]bool{}
	for _, host := range spec.Hosts {
		if _, err := ParseView(string(host)); err != nil {
			return View{}, err
		}
		if spec.Ref.Kind() != "command" || host == spec.Ref || seenHosts[host] {
			return View{}, fmt.Errorf("screen: invalid host %s for %s", host, spec.Ref)
		}
		seenHosts[host] = true
	}
	v := View{ref: spec.Ref, cardinality: spec.Cardinality, hosts: append([]ViewRef{}, spec.Hosts...), anchors: []Anchor{}}
	seen := map[string]bool{}
	for _, input := range spec.Anchors {
		a, err := NewAnchor(input)
		if err != nil {
			return View{}, fmt.Errorf("screen: %s: %w", spec.Ref, err)
		}
		if seen[a.Name()] {
			return View{}, fmt.Errorf("screen: duplicate anchor %s#%s", spec.Ref, a.Name())
		}
		seen[a.Name()] = true
		v.anchors = append(v.anchors, a)
	}
	return v, nil
}

// MustView constructs a trusted declaration and panics on an invalid contract.
func MustView(spec ViewSpec) View {
	v, err := NewView(spec)
	if err != nil {
		panic(err)
	}
	return v
}

func (v View) Ref() ViewRef             { return v.ref }
func (v View) Cardinality() Cardinality { return v.cardinality }
func (v View) Hosts() []ViewRef         { return slices.Clone(v.hosts) }
func (v View) Anchors() []Anchor        { return slices.Clone(v.anchors) }

// Anchor resolves an exact full reference and refuses missing or foreign names.
func (v View) Anchor(ref AnchorRef) (Anchor, error) {
	if _, err := ParseAnchor(string(ref)); err != nil {
		return Anchor{}, err
	}
	view, name, _ := strings.Cut(string(ref), "#")
	if view == string(v.ref) {
		for _, a := range v.anchors {
			if a.Name() == name {
				return a, nil
			}
		}
	}
	return Anchor{}, fmt.Errorf("screen: missing anchor %s", ref)
}

func bindingControl(binding, control string) bool {
	switch binding {
	case "navigation":
		return control == "link"
	case "submission":
		return control == "button"
	case "display":
		return control != "link" && control != "button"
	case "value":
		return control != "link" && control != "button" && control != "display"
	}
	return false
}
