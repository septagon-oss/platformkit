package style

// State is a typed CSS modifier state (hover, focus, disabled, etc.).
// Used with ClassList.On() to wrap child classes in a Tailwind prefix
// such as "hover:" or "focus-visible:".
type State string

// Prefix returns the Tailwind prefix including the trailing colon
// (e.g., "hover:"). Returns empty string for the zero value.
func (s State) Prefix() string {
	if s == "" {
		return ""
	}
	return string(s) + ":"
}

// The state modifiers this application uses. Eleven more were declared and
// nothing ever named one — a vocabulary is a list of what a component may say,
// and a word with no speaker is a word a reader has to look up. A component that
// needs first:, odd: or group-hover: adds it back in the commit that uses it,
// which is also the commit that makes the CSS carry the rule.
const (
	StateHover        State = "hover"
	StateFocus        State = "focus"
	StateFocusVisible State = "focus-visible"
	StateDisabled     State = "disabled"
	StatePlaceholder  State = "placeholder"
)
