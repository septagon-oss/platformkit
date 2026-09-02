package style

// State is a typed CSS modifier state (hover, focus, disabled, etc.).
// Used with ClassList.On() to wrap child classes in a Tailwind prefix
// such as "hover:" or "focus-visible:".
type State string

// IsZero reports whether the State is the zero value (no prefix).
func (s State) IsZero() bool { return s == "" }

// Prefix returns the Tailwind prefix including the trailing colon
// (e.g., "hover:"). Returns empty string for the zero value.
func (s State) Prefix() string {
	if s == "" {
		return ""
	}
	return string(s) + ":"
}

// String returns the canonical state key.
func (s State) String() string { return string(s) }

// Canonical state modifiers.
const (
	StateHover        State = "hover"
	StateFocus        State = "focus"
	StateFocusVisible State = "focus-visible"
	StateFocusWithin  State = "focus-within"
	StateActive       State = "active"
	StateDisabled     State = "disabled"
	StateChecked      State = "checked"
	StateFirst        State = "first"
	StateLast         State = "last"
	StateOdd          State = "odd"
	StateEven         State = "even"
	StateGroupHover   State = "group-hover"
	StateGroupFocus   State = "group-focus"
	StatePeer         State = "peer"
	StatePlaceholder  State = "placeholder"
	StateDark         State = "dark"
)

// AllStates returns every State const in stable order.
func AllStates() []State {
	return []State{
		StateHover, StateFocus, StateFocusVisible, StateFocusWithin,
		StateActive, StateDisabled, StateChecked,
		StateFirst, StateLast, StateOdd, StateEven,
		StateGroupHover, StateGroupFocus, StatePeer,
		StatePlaceholder, StateDark,
	}
}
