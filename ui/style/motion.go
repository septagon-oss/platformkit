package style

// Transition is a typed CSS transition property group.
type Transition string

const (
	TransitionNone      Transition = "none"
	TransitionAll       Transition = "all"
	TransitionColors    Transition = "colors"
	TransitionOpacity   Transition = "opacity"
	TransitionShadow    Transition = "shadow"
	TransitionTransform Transition = "transform"
)

// Duration is a typed transition duration step.
type Duration string

const (
	Duration75   Duration = "75"
	Duration100  Duration = "100"
	Duration150  Duration = "150"
	Duration200  Duration = "200"
	Duration300  Duration = "300"
	Duration500  Duration = "500"
	Duration700  Duration = "700"
	Duration1000 Duration = "1000"
)

// Easing is a typed transition easing curve.
type Easing string

const (
	EaseLinear Easing = "linear"
	EaseIn     Easing = "in"
	EaseOut    Easing = "out"
	EaseInOut  Easing = "in-out"
)

// AllTransitions returns every Transition const in stable order.
func AllTransitions() []Transition {
	return []Transition{
		TransitionNone, TransitionAll, TransitionColors,
		TransitionOpacity, TransitionShadow, TransitionTransform,
	}
}

// AllDurations returns every Duration const in stable order.
func AllDurations() []Duration {
	return []Duration{
		Duration75, Duration100, Duration150, Duration200,
		Duration300, Duration500, Duration700, Duration1000,
	}
}

// AllEasings returns every Easing const in stable order.
func AllEasings() []Easing {
	return []Easing{EaseLinear, EaseIn, EaseOut, EaseInOut}
}
