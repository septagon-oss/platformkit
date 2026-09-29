package export

import (
	"fmt"
	"maps"
	"slices"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui/style"
)

// TokenMode holds selected theme colours, ordered fallback families and the
// theme's own shape lengths under their CSS identities. A client sets its radii
// through design.Client, so a projection that stopped at colour and font would
// ship the client's palette and drop the client's shape — and the document
// platformkit-mobile reads is this projection.
type TokenMode struct {
	Mode       string                   `json:"mode"`
	Colors     []design.Token           `json:"colors,omitempty"`
	Fonts      []design.FontFamilyToken `json:"fonts,omitempty"`
	Dimensions []design.Token           `json:"dimensions,omitempty"`
}

// TokenExport composes existing declaration owners as read-only source data.
// Colors are shared declarations resolved separately in each selected mode;
// scales and timing keep their existing owner-local keys and explicit units.
// Callers may select dependency-closed subsets and supply asset metadata. This
// is not a registry, source-editing API, CSS equivalence or provider approval.
type TokenExport struct {
	Modes       []TokenMode             `json:"modes,omitempty"`
	Colors      []design.ColorToken     `json:"colors,omitempty"`
	Scales      []style.ScaleValue      `json:"scales,omitempty"`
	Shadows     []style.ShadowValue     `json:"shadows,omitempty"`
	Easings     []style.EasingValue     `json:"easings,omitempty"`
	Transitions []style.TransitionValue `json:"transitions,omitempty"`
	Assets      []design.Asset          `json:"assets,omitempty"`
	Faces       []design.FontFace       `json:"faces,omitempty"`
}

// checkLegible runs the contrast gate over the layer a reader is actually shown:
// the role declarations ui/style emits on top of this pair's tokens. design's own
// Theme.Check measures the tokens and belongs to whoever loads a client's design
// (design.Client.Resolve); this half is the one a pair can pass the tokens while
// still painting an unreadable sentence — a component never sets
// --pk-color-text-primary, it sets color: var(--pk-role-fg-secondary). Client.
// Resolve runs both halves today, so this is not the only place a pair is
// measured; it is the seam where a pair becomes a stylesheet, an export or a
// Storybook, and it refuses whatever produced the pair.
func checkLegible(context string, themes ...design.Theme) error {
	roles, pairs := style.RoleColors(), style.GatedRolePairs()
	for _, theme := range themes {
		if err := theme.CheckRoles(roles, pairs); err != nil {
			return fmt.Errorf("%s: %w", context, err)
		}
	}
	return nil
}

// ExportTokens projects only explicitly selected light/dark modes, in selector
// order, plus the existing shared colour, scale, shadow and timing owners. It
// captures no components, CSS, icons or font bytes and performs no I/O. Failure
// returns no partial output; every returned nested value is detached.
func ExportTokens(theme design.Pair, modes ...string) (TokenExport, error) {
	selected := make(map[string]bool, len(modes))
	for _, mode := range modes {
		if (mode != "light" && mode != "dark") || selected[mode] {
			return TokenExport{}, fmt.Errorf("token export: invalid or duplicate mode %q", mode)
		}
		selected[mode] = true
	}
	if len(selected) == 0 {
		return TokenExport{}, fmt.Errorf("token export: select at least one mode")
	}
	var chosen []design.Theme
	for i, mode := range []string{"light", "dark"} {
		if selected[mode] {
			chosen = append(chosen, theme.Both()[i])
		}
	}
	if err := checkLegible("token export", chosen...); err != nil {
		return TokenExport{}, err
	}
	out := TokenExport{Colors: style.RoleColors()}
	for i, mode := range []string{"light", "dark"} {
		if !selected[mode] {
			continue
		}
		owner := theme.Both()[i]
		fonts, err := owner.FontFamilies()
		if err != nil {
			return TokenExport{}, fmt.Errorf("token export mode %s: %w", mode, err)
		}
		value := TokenMode{Mode: mode, Fonts: fonts}
		for _, token := range owner.Tokens() {
			switch token.Type {
			case "color":
				value.Colors = append(value.Colors, token)
			case "dimension":
				// The theme is trusted Go input, but the document is not: a radius
				// in a unit DTCG cannot carry would reach a consumer as a string.
				if _, _, err := design.ParseRadius(token.Value); err != nil {
					return TokenExport{}, fmt.Errorf("token export mode %s: %w", mode, err)
				}
				value.Dimensions = append(value.Dimensions, token)
			}
		}
		out.Modes = append(out.Modes, value)
	}
	var err error
	if out.Scales, err = style.ScaleValues(); err != nil {
		return TokenExport{}, err
	}
	if out.Shadows, err = style.ShadowValues(); err != nil {
		return TokenExport{}, err
	}
	if out.Easings, err = style.EasingValues(); err != nil {
		return TokenExport{}, err
	}
	if out.Transitions, err = style.TransitionValues(); err != nil {
		return TokenExport{}, err
	}
	if err := out.Validate(); err != nil {
		return TokenExport{}, err
	}
	return out, nil
}

// Validate checks selected identities, values and dependency closure without
// mutation. Modes must expose the same colour/font identities, but may use
// different values. Assets or scales alone need no mode, component or layout.
// Font fallback names do not assert that matching physical faces are supplied.
func (s TokenExport) Validate() error {
	if len(s.Colors) != 0 && len(s.Modes) == 0 {
		return fmt.Errorf("token export: shared colours require a selected mode")
	}
	modes := make(map[string]bool, len(s.Modes))
	var firstKinds map[string]string
	for _, mode := range s.Modes {
		if (mode.Mode != "light" && mode.Mode != "dark") || modes[mode.Mode] {
			return fmt.Errorf("token export: invalid or duplicate mode %q", mode.Mode)
		}
		modes[mode.Mode] = true
		base := slices.Clone(mode.Colors)
		for _, color := range base {
			if color.Type != "color" {
				return fmt.Errorf("token export mode %s: non-colour %q", mode.Mode, color.Name)
			}
		}
		for _, font := range mode.Fonts {
			if err := font.Validate(); err != nil {
				return fmt.Errorf("token export mode %s: %w", mode.Mode, err)
			}
			base = append(base, design.Token{Name: font.Name, Type: "fontFamily"})
		}
		if _, err := design.ResolveColors(base, s.Colors); err != nil {
			return fmt.Errorf("token export mode %s: %w", mode.Mode, err)
		}
		kinds := make(map[string]string, len(base))
		for _, token := range base {
			kinds[token.Name] = token.Type
		}
		// Shape travels with the palette, so it keeps the same identity rule: a
		// radius is a dimension this format can express, and both modes expose the
		// same shape identities as each other (kinds is compared below).
		for _, dimension := range mode.Dimensions {
			if dimension.Type != "dimension" || !slices.Contains(design.RadiusTokenNames(), dimension.Name) {
				return fmt.Errorf("token export mode %s: shape token %q is not a radius dimension", mode.Mode, dimension.Name)
			}
			if _, _, err := design.ParseRadius(dimension.Value); err != nil {
				return fmt.Errorf("token export mode %s: %w", mode.Mode, err)
			}
			if kinds[dimension.Name] != "" {
				return fmt.Errorf("token export mode %s: duplicate token %q", mode.Mode, dimension.Name)
			}
			kinds[dimension.Name] = dimension.Type
		}
		if firstKinds == nil {
			firstKinds = kinds
		} else if !maps.Equal(firstKinds, kinds) {
			return fmt.Errorf("token export mode %s: colour/font identities differ across modes", mode.Mode)
		}
	}
	seen := make(map[string]bool)
	check := func(key string, err error) error {
		if err != nil {
			return fmt.Errorf("token export %s: %w", key, err)
		}
		if seen[key] {
			return fmt.Errorf("token export: duplicate %s", key)
		}
		seen[key] = true
		return nil
	}
	for _, v := range s.Scales {
		if err := check("scale/"+v.Scale+"/"+v.Key, v.Validate()); err != nil {
			return err
		}
	}
	for _, v := range s.Shadows {
		if err := check("shadow/"+v.Key, v.Validate()); err != nil {
			return err
		}
	}
	for _, v := range s.Easings {
		if err := check("easing/"+v.Key, v.Validate()); err != nil {
			return err
		}
	}
	for _, v := range s.Transitions {
		if err := check("transition/"+v.Key, v.Validate()); err != nil {
			return err
		}
		if (v.Duration != "" && !seen["scale/duration/"+v.Duration]) || (v.Easing != "" && !seen["easing/"+v.Easing]) {
			return fmt.Errorf("token export transition %s: duration/easing is not selected", v.Key)
		}
	}
	return design.ValidateAssets(s.Assets, s.Faces)
}
