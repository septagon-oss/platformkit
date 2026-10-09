package components

import (
	"fmt"
	"io"
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

var clChoice = clRecoveryAction.Merge(clTableSelection)

// StateSlots contains only public recovery actions, never the preceding result.
type StateSlots struct{ EmptyAction, RetryAction []g.Node }

func invalidComponent(err error) g.Node { return g.NodeFunc(func(io.Writer) error { return err }) }

func required(values ...string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return false
		}
	}
	return true
}

func aggregateState(label string, state ContentState, payload bool) error {
	if !required(label) {
		return fmt.Errorf("component: Label is required")
	}
	if err := state.Validate(); err != nil {
		return err
	}
	if !state.ready() && payload {
		return fmt.Errorf("component: absent states must clear protected data")
	}
	return nil
}

// retainedNavigation reports whether a read that returned no result still carries
// the navigation into one. View switches, range chips and billing-period toggles
// are places inside the result set that is no longer displayed: the HTML omits
// them, but a typed capture of the same Props would export them.
// An empty read keeps its navigation, because its result is present; loading
// keeps it, because the same read is still outstanding.
func retainedNavigation(status MediaStatus, choices []ChoiceLink, links ...*ChoiceLink) bool {
	if !absentRead(status) {
		return false
	}
	if len(choices) != 0 {
		return true
	}
	for _, link := range links {
		if link != nil {
			return true
		}
	}
	return false
}

// retainedDayStrip reports the same verdict for a strip of days: every day's href
// is a route into the range the read no longer has, and the strip's own paging
// links are two more. The days are a []DateChoice rather than []ChoiceLink, so
// they join the rule here rather than at each call site.
func retainedDayStrip(status MediaStatus, strip DateStripProps) bool {
	return absentRead(status) && (len(strip.Days) != 0 || strip.Previous != nil || strip.Next != nil)
}

// absentRead is the half of the rule both helpers decide on: a read that failed or
// was refused returned no result to hold navigation for.
func absentRead(status MediaStatus) bool { return status == MediaFailed || status == MediaRefused }

// navigationLink wraps content in its destination. A disabled component keeps the
// content readable but offers no destination to follow: an anchor with no href is
// inert to pointer, keyboard and enhancement alike, which is the shape a disabled
// navigation choice takes everywhere else.
func navigationLink(disabled bool, href string, children ...g.Node) g.Node {
	if disabled {
		return h.A(append([]g.Node{h.Role("link"), g.Attr("aria-disabled", "true"), g.Attr("tabindex", "-1")}, children...)...)
	}
	return h.A(append([]g.Node{h.Href(href)}, children...)...)
}

func sharedSection(p ComponentProps, name, label string, children ...g.Node) g.Node {
	nodes := append(baseAttrs(p), classes(clDataList.Compile(), p.Class), g.Attr("data-component", name),
		g.Attr("data-shared-content", ""), g.Attr("aria-label", label))
	return h.Section(append(nodes, children...)...)
}

func stateBody(state ContentState, slots StateSlots) g.Node {
	return contentStateNode(state, slots.EmptyAction, slots.RetryAction, Skeleton(SkeletonProps{Shape: "text", Lines: 3}))
}

type Step struct {
	Key         string `json:"key"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	State       string `json:"state,omitempty" enum:",upcoming,complete,error,disabled"`
	Href        string `json:"href,omitempty"`
}

type StepperProps struct {
	ComponentProps
	Label          string       `json:"label"`
	State          ContentState `json:"state,omitzero"`
	Steps          []Step       `json:"steps,omitempty"`
	CurrentKey     string       `json:"currentKey,omitempty"`
	ProgressText   string       `json:"progressText"`
	Busy           bool         `json:"busy,omitzero"`
	FormID         string       `json:"formID,omitempty"`
	SaveStatusText string       `json:"saveStatusText,omitempty"`
}

type StepperSlots struct {
	StateSlots
	Body, Back, Continue, SaveExit, Cancel, ErrorSummary []g.Node
}

func (p StepperProps) Validate() error {
	if err := aggregateState(p.Label, p.State, len(p.Steps) > 0 || p.CurrentKey != ""); err != nil {
		return err
	}
	if p.State.Status == MediaEmpty {
		return fmt.Errorf("Stepper: an empty sequence is not a completed flow")
	}
	if !p.State.ready() {
		return nil
	}
	if len(p.Steps) == 0 || !required(p.ProgressText) {
		return fmt.Errorf("Stepper: steps and progress copy are required")
	}
	seen, current := map[string]bool{}, false
	for _, step := range p.Steps {
		if !required(step.Key, step.Title) || seen[step.Key] {
			return fmt.Errorf("Stepper: unique keys and titles are required")
		}
		seen[step.Key] = true
		switch step.State {
		case "", "upcoming", "complete", "error", "disabled":
		default:
			return fmt.Errorf("Stepper: unknown step state")
		}
		if step.Key == p.CurrentKey {
			current = step.State != "disabled"
		}
	}
	if !current {
		return fmt.Errorf("Stepper: CurrentKey must name an enabled step")
	}
	return nil
}

func Stepper(p StepperProps) g.Node { return StepperWithSlots(p, StepperSlots{}) }

func StepperWithSlots(p StepperProps, slots StepperSlots) g.Node {
	if err := p.Validate(); err != nil {
		return invalidComponent(err)
	}
	if !p.State.ready() {
		if len(slots.Body)+len(slots.ErrorSummary)+len(slots.Back)+len(slots.Continue)+len(slots.SaveExit)+len(slots.Cancel) > 0 {
			return invalidComponent(fmt.Errorf("Stepper: absent states must clear protected slots"))
		}
		return sharedSection(p.ComponentProps, "stepper", p.Label, stateBody(p.State, slots.StateSlots))
	}
	completed := 0
	var items []g.Node
	for _, step := range p.Steps {
		if step.State == "complete" {
			completed++
		}
		attrs := []g.Node{g.Attr("data-step-key", step.Key), g.Attr("data-step-state", step.State)}
		if step.Key == p.CurrentKey {
			attrs = append(attrs, g.Attr("aria-current", "step"))
		}
		var title g.Node = g.Text(step.Title)
		if step.Href != "" && step.State != "disabled" {
			title = recoveryAction(ButtonProps{Label: step.Title, Href: step.Href, Variant: "link"}, p.Disabled)
		}
		items = append(items, h.Li(append(attrs, title, g.If(step.Description != "", Text(TextProps{Content: step.Description, Size: "sm"})))...))
	}
	body := []g.Node{stateBody(p.State, slots.StateSlots),
		h.Progress(g.Attr("value", itoa(completed)), h.Max(itoa(len(p.Steps))), g.Attr("aria-label", p.ProgressText)),
		Text(TextProps{Content: p.ProgressText, Size: "sm"}),
		h.Details(h.Open(), h.Summary(h.Class(clDataDisclosure.Compile()), g.Text(p.Label)), h.Nav(g.Attr("aria-label", p.Label), h.Ol(h.Class(clDataList.Compile()), g.Group(items)))),
		g.Group(slots.ErrorSummary), h.FieldSet(g.If(p.FormID != "", g.Attr("form", p.FormID)), g.If(p.Disabled || p.Busy, h.Disabled()), g.Group(slots.Body)),
		Flex(FlexProps{Wrap: true, Gap: "3"}, g.Group(slots.Back), g.Group(slots.Continue), g.Group(slots.SaveExit), g.Group(slots.Cancel)),
		h.P(h.Role("status"), g.Attr("aria-live", "polite"), g.Attr("aria-busy", boolText(p.Busy)), g.Text(p.SaveStatusText))}
	return sharedSection(p.ComponentProps, "stepper", p.Label, body...)
}
