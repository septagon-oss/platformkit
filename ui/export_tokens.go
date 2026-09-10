package ui

import (
	"fmt"
	"maps"
	"slices"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui/style"
)

// TokenMode holds selected theme colours and ordered fallback families under
// their CSS identities. Other theme token kinds are outside this projection.
type TokenMode struct {
	Mode   string                   `json:"mode"`
	Colors []design.Token           `json:"colors,omitempty"`
	Fonts  []design.FontFamilyToken `json:"fonts,omitempty"`
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
			if token.Type == "color" {
				value.Colors = append(value.Colors, token)
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
