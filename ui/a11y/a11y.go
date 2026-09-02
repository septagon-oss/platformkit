// Package a11y is the ARIA vocabulary the components speak: the roles and
// states a screen reader reads, as typed values that render to attributes.
//
// It is attributes and nothing else. The version this is derived from also
// carried a process-global announcement manager, a focus stack, a warning
// validator, a focus trap that emitted JavaScript from Go, and a contrast
// checker with no caller — 798 lines of which the renderers used none. What is
// here is what the components and the admin shell actually attach to an
// element, and every one of them has a caller.
//
// The two things a person needs that are not attributes — the skip link and the
// live region — are components, in ui/components, because they have a size and
// a colour and those come from a declared class list. A widget styled with a
// hand-written class string is a widget whose CSS is not in the stylesheet.
//
// Derived from github.com/septagon-oss/pk-ui (Apache-2.0); see NOTICE.
package a11y

import (
	"strconv"
	"sync/atomic"

	g "maragu.dev/gomponents"
)

// Role is an ARIA role. The list is the roles this application's widgets take,
// not the whole specification: a constant nothing sets is a constant that says
// nothing about what is here.
type Role string

const (
	RoleAlert        Role = "alert"
	RoleStatus       Role = "status"
	RoleDialog       Role = "dialog"
	RoleNavigation   Role = "navigation"
	RoleBanner       Role = "banner"
	RoleContentinfo  Role = "contentinfo"
	RoleMain         Role = "main"
	RoleSearch       Role = "search"
	RoleTablist      Role = "tablist"
	RoleTab          Role = "tab"
	RoleTabpanel     Role = "tabpanel"
	RoleGroup        Role = "group"
	RoleList         Role = "list"
	RoleListitem     Role = "listitem"
	RolePresentation Role = "presentation"
	RoleRegion       Role = "region"
	RoleSeparator    Role = "separator"
	RoleProgressbar  Role = "progressbar"
	RoleButton       Role = "button"
)

// State is the value of a tri-state ARIA attribute. It is a type rather than a
// bool because "mixed" is a third answer and aria-checked has three.
type State string

const (
	True  State = "true"
	False State = "false"
	Mixed State = "mixed"
)

// Bool is the State for a boolean condition, so a caller writes the condition
// rather than the spelling of its answer.
func Bool(b bool) *State {
	if b {
		s := True
		return &s
	}
	s := False
	return &s
}

// Props are the ARIA attributes of one element. Pointer fields are the
// tri-states: absent is different from false, because aria-expanded="false"
// says there is something to expand and no attribute says there is not.
type Props struct {
	Role        Role
	Label       string
	LabelledBy  string
	DescribedBy string

	Checked  *State
	Disabled *State
	Expanded *State
	Hidden   *State
	Invalid  *State
	Pressed  *State
	Required *State
	Selected *State
	Busy     *State

	HasPopup string
	Live     string
	Atomic   *State
	Controls string
	Owns     string
	Current  string

	ValueMin  *int
	ValueMax  *int
	ValueNow  *int
	ValueText string

	Level    *int
	PosInSet *int
	SetSize  *int

	Orientation string
	Sort        string
	TabIndex    *int
}

// Attributes renders the properties, in a fixed order so that two renders of
// one element are the same bytes and a golden test means something.
func (p Props) Attributes() []g.Node {
	var out []g.Node
	str := func(name, v string) {
		if v != "" {
			out = append(out, g.Attr(name, v))
		}
	}
	state := func(name string, v *State) {
		if v != nil {
			out = append(out, g.Attr(name, string(*v)))
		}
	}
	number := func(name string, v *int) {
		if v != nil {
			out = append(out, g.Attr(name, strconv.Itoa(*v)))
		}
	}
	str("role", string(p.Role))
	str("aria-label", p.Label)
	str("aria-labelledby", p.LabelledBy)
	str("aria-describedby", p.DescribedBy)
	state("aria-checked", p.Checked)
	state("aria-disabled", p.Disabled)
	state("aria-expanded", p.Expanded)
	state("aria-hidden", p.Hidden)
	state("aria-invalid", p.Invalid)
	state("aria-pressed", p.Pressed)
	state("aria-required", p.Required)
	state("aria-selected", p.Selected)
	state("aria-busy", p.Busy)
	str("aria-haspopup", p.HasPopup)
	str("aria-live", p.Live)
	state("aria-atomic", p.Atomic)
	str("aria-controls", p.Controls)
	str("aria-owns", p.Owns)
	str("aria-current", p.Current)
	number("aria-valuemin", p.ValueMin)
	number("aria-valuemax", p.ValueMax)
	number("aria-valuenow", p.ValueNow)
	str("aria-valuetext", p.ValueText)
	number("aria-level", p.Level)
	number("aria-posinset", p.PosInSet)
	number("aria-setsize", p.SetSize)
	str("aria-orientation", p.Orientation)
	str("aria-sort", p.Sort)
	number("tabindex", p.TabIndex)
	return out
}

// ids numbers the generated identifiers of one process.
var ids atomic.Uint64

// ID is a unique element id with the given prefix, for the times one element
// has to point at another — a field at its error, a dialog at its title — and
// neither has a name of its own.
//
// It is a counter and not a hash of anything, so it is unique within a process
// and nothing more: an id that had to be stable across renders would be a
// caller's business, and that caller has a name to use.
func ID(prefix string) string {
	return prefix + "-" + strconv.FormatUint(ids.Add(1), 10)
}
