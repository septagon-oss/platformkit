// Package moduleplan checks module selections against caller-owned definitions.
// It neither registers constructors nor enables dependencies implicitly.
package moduleplan

import (
	"fmt"
	"maps"
	"slices"
)

// Validate checks that order names every defined module exactly once and places
// each dependency before its consumer. Inputs are never modified.
func Validate(order []string, requirements map[string][]string) error {
	positions := make(map[string]int, len(order))
	for i, name := range order {
		if name == "" {
			return fmt.Errorf("construction order contains an empty module name")
		}
		if _, exists := requirements[name]; !exists {
			return fmt.Errorf("construction order names %q, which is absent from the module definitions", name)
		}
		if _, exists := positions[name]; exists {
			return fmt.Errorf("construction order must name %q exactly once", name)
		}
		positions[name] = i
	}
	for _, name := range slices.Sorted(maps.Keys(requirements)) {
		if name == "" {
			return fmt.Errorf("module definitions contain an empty name")
		}
		if _, exists := positions[name]; !exists {
			return fmt.Errorf("construction order omits module %q", name)
		}
	}
	for i, name := range order {
		seen := make(map[string]bool, len(requirements[name]))
		for _, need := range requirements[name] {
			if need == "" {
				return fmt.Errorf("module %q has an empty dependency", name)
			}
			if seen[need] {
				return fmt.Errorf("module %q declares dependency %q twice", name, need)
			}
			seen[need] = true
			position, exists := positions[need]
			if !exists {
				return fmt.Errorf("module %q is built from %q, which is absent from the module definitions", name, need)
			}
			if position >= i {
				return fmt.Errorf("module %q is built from %q, which must appear earlier in construction order", name, need)
			}
		}
	}
	return nil
}

// Select validates a complete definition and an explicit selection, including
// mandatory modules and dependencies. It returns a fresh slice in construction
// order. An error returns no selection; no missing module is enabled implicitly.
func Select(order []string, requirements map[string][]string, mandatory, include []string) ([]string, error) {
	if err := Validate(order, requirements); err != nil {
		return nil, err
	}
	want := make(map[string]bool, len(include))
	for _, name := range include {
		if name == "" {
			return nil, fmt.Errorf("selection contains an empty module name")
		}
		if _, exists := requirements[name]; !exists {
			return nil, fmt.Errorf("selected module %q is absent from the module definitions", name)
		}
		if want[name] {
			return nil, fmt.Errorf("selected module %q appears twice", name)
		}
		want[name] = true
	}
	for _, name := range mandatory {
		if !want[name] {
			return nil, fmt.Errorf("selection does not include %q; it is mandatory", name)
		}
	}
	selected := make([]string, 0, len(include))
	for _, name := range order {
		if !want[name] {
			continue
		}
		for _, need := range requirements[name] {
			if !want[need] {
				return nil, fmt.Errorf("selection includes %q, which is built from %q, and does not include it", name, need)
			}
		}
		selected = append(selected, name)
	}
	return selected, nil
}
