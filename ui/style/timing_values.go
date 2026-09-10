package style

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
)

// EasingValue retains either the linear keyword or four cubic control points.
// Only X coordinates are bounded to [0,1]; Y may overshoot. Other CSS easing
// functions are not described by this source contract.
type EasingValue struct {
	Key         string      `json:"key"`
	Keyword     string      `json:"keyword,omitempty"`
	CubicBezier *[4]float64 `json:"cubicBezier,omitempty"`
}

func (v EasingValue) Validate() error {
	valid := slices.Contains(AllEasings(), Easing(v.Key))
	if v.CubicBezier == nil {
		valid = valid && v.Keyword == "linear"
	} else {
		valid = valid && v.Keyword == ""
		for i, n := range v.CubicBezier {
			valid = valid && !math.IsNaN(n) && !math.IsInf(n, 0) && (i%2 != 0 || (n >= 0 && n <= 1))
		}
	}
	if !valid {
		return fmt.Errorf("style: invalid easing %q", v.Key)
	}
	return nil
}

func (v EasingValue) css() string {
	if v.CubicBezier == nil {
		return v.Keyword
	}
	var points []string
	for _, n := range v.CubicBezier {
		points = append(points, decimal(n))
	}
	return "cubic-bezier(" + strings.Join(points, ", ") + ")"
}

// EasingValues returns validated, detached values from CSS's timing owner.
func EasingValues() ([]EasingValue, error) {
	var out []EasingValue
	for _, key := range AllEasings() {
		v := easings[string(key)]
		v.Key = string(key)
		if v.CubicBezier != nil {
			v.CubicBezier = new(*v.CubicBezier)
		}
		if err := v.Validate(); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// TransitionValue retains the ordered property group and its declared timing
// references: Duration names the duration scale and Easing names EasingValues.
// Empty references mean absent declarations (transition-none), not CSS defaults.
type TransitionValue struct {
	Key        string   `json:"key"`
	Properties []string `json:"properties"`
	Duration   string   `json:"duration,omitempty"`
	Easing     string   `json:"easing,omitempty"`
}

var transitionProperty = regexp.MustCompile(`^(--)?[a-z][a-z0-9-]*$`)

func (v TransitionValue) Validate() error {
	valid := slices.Contains(AllTransitions(), Transition(v.Key)) && len(v.Properties) > 0
	seen := make(map[string]bool)
	for _, property := range v.Properties {
		valid = valid && transitionProperty.MatchString(property) && !seen[property] &&
			((property != "all" && property != "none") || len(v.Properties) == 1)
		seen[property] = true
	}
	if v.Key == "none" {
		valid = valid && slices.Equal(v.Properties, []string{"none"}) && v.Duration == "" && v.Easing == ""
	} else {
		valid = valid && !slices.Contains(v.Properties, "none") && slices.Contains(AllDurations(), Duration(v.Duration)) && slices.Contains(AllEasings(), Easing(v.Easing))
	}
	if !valid {
		return fmt.Errorf("style: invalid transition %q", v.Key)
	}
	return nil
}

// TransitionValues shares CSS's property groups and default timing references.
// It describes declarations, not cascade precedence or native motion support.
func TransitionValues() ([]TransitionValue, error) {
	var out []TransitionValue
	for _, key := range AllTransitions() {
		v := TransitionValue{Key: string(key), Properties: strings.Split(transitionProps[string(key)], ", ")}
		if key != TransitionNone {
			v.Duration, v.Easing = string(transitionDuration), string(transitionEasing)
		}
		if err := v.Validate(); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
