package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/style"
)

var ErrSourceUnsupported = errors.New("source snapshot contract is unsupported")

const tokenFeature = "source-tokens.v1"

// WithTokens attaches a validated selection to an existing v1/v2 capture,
// advertises its required feature and hashes a detached snapshot. Selected
// theme identities and values must agree with the original capture. Unknown
// layout stays unknown; capture does not certify downstream projection support.
func (d DesignExport) WithTokens(tokens TokenExport) (DesignExport, error) {
	if d.SourceTokens != nil || (d.Schema != "platformkit.design-export.v1" && d.Schema != "platformkit.design-export.v2") ||
		(d.Schema == "platformkit.design-export.v1" && len(d.RequiredFeatures) != 0) ||
		(d.Schema == "platformkit.design-export.v2" && len(d.RequiredFeatures) == 0) {
		return DesignExport{}, fmt.Errorf("%w: token attachment requires an existing unextended capture", ErrSourceUnsupported)
	}
	d.Schema = "platformkit.design-export.v2"
	d.RequiredFeatures = append(slices.Clone(d.RequiredFeatures), tokenFeature)
	d.SourceTokens = &tokens
	if err := d.checkSourceFeatures("source-flex-declarations.v1", "source-measurements.v1", tokenFeature); err != nil {
		return DesignExport{}, err
	}
	if err := d.checkTokenBindings(); err != nil {
		return DesignExport{}, err
	}
	d.SHA256 = ""
	payload, err := json.Marshal(d)
	if err != nil {
		return DesignExport{}, fmt.Errorf("%w: encode snapshot: %w", ErrSourceUnsupported, err)
	}
	var out DesignExport
	if err := json.Unmarshal(payload, &out); err != nil {
		return DesignExport{}, fmt.Errorf("%w: detach snapshot: %w", ErrSourceUnsupported, err)
	}
	out.SHA256 = digest(payload)
	return out, nil
}

// CheckSourceContract composes the understood v2 feature gates. Token-only
// captures need no layout. Earlier layout-only v2 captures retain their existing
// admission rules. This checks trusted Go values, not JSON decoding, SHA-256,
// CSS equivalence, native capabilities, physical assets or authority to write.
func (d DesignExport) CheckSourceContract(supported ...string) error {
	if err := d.checkSourceFeatures(supported...); err != nil {
		return err
	}
	if err := d.checkTokenBindings(); err != nil {
		return err
	}
	if slices.Contains(d.RequiredFeatures, "source-flex-declarations.v1") {
		// Only dispatch the layout owner's features after the whole envelope
		// has passed. Its public gate still refuses features it cannot check.
		d.RequiredFeatures = slices.DeleteFunc(slices.Clone(d.RequiredFeatures), func(f string) bool { return f == tokenFeature })
		return d.CheckLayoutContract(supported...)
	}
	return nil
}

func (d DesignExport) checkSourceFeatures(supported ...string) error {
	if d.Schema != "platformkit.design-export.v2" || len(d.RequiredFeatures) == 0 {
		return fmt.Errorf("%w: schema or missing features", ErrSourceUnsupported)
	}
	required := make(map[string]bool)
	for _, feature := range d.RequiredFeatures {
		if required[feature] || !slices.Contains([]string{"source-flex-declarations.v1", "source-measurements.v1", tokenFeature}, feature) || !slices.Contains(supported, feature) {
			return fmt.Errorf("%w: required feature %q", ErrSourceUnsupported, feature)
		}
		required[feature] = true
	}
	if required[tokenFeature] != (d.SourceTokens != nil) ||
		(required["source-measurements.v1"] && !required["source-flex-declarations.v1"]) ||
		(len(d.Measurements) != 0 && !required["source-measurements.v1"]) {
		return fmt.Errorf("%w: feature/data mismatch", ErrSourceUnsupported)
	}
	var checkLayoutPresence func([]components.ExampleDescription) error
	checkLayoutPresence = func(examples []components.ExampleDescription) error {
		for _, example := range examples {
			if example.Layout != nil && !required["source-flex-declarations.v1"] {
				return fmt.Errorf("%w: unadvertised layout at %q", ErrSourceUnsupported, example.ID)
			}
			for _, child := range example.Children {
				if err := checkLayoutPresence([]components.ExampleDescription{child.Description}); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return checkLayoutPresence(d.Examples)
}

func (d DesignExport) checkTokenBindings() error {
	if d.SourceTokens == nil {
		return nil
	}
	if err := d.SourceTokens.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrSourceUnsupported, err)
	}
	measurements := make(map[[2]string]style.Measurement, len(d.Measurements))
	for _, measure := range d.Measurements {
		measurements[[2]string{measure.Scale, measure.Key}] = measure
	}
	for _, scale := range d.SourceTokens.Scales {
		measure, exists := measurements[[2]string{scale.Scale, scale.Key}]
		if exists && (scale.Number == nil || scale.Number.Value != measure.Value || scale.Number.Unit != measure.Unit) {
			return fmt.Errorf("%w: scale %s/%s differs from captured measurement", ErrSourceUnsupported, scale.Scale, scale.Key)
		}
	}
	themes := make(map[string]ThemeExport, len(d.Themes))
	for _, theme := range d.Themes {
		if _, exists := themes[theme.Mode]; exists {
			return fmt.Errorf("%w: duplicate theme mode %q", ErrSourceUnsupported, theme.Mode)
		}
		themes[theme.Mode] = theme
	}
	for _, mode := range d.SourceTokens.Modes {
		theme, exists := themes[mode.Mode]
		if !exists {
			return fmt.Errorf("%w: missing theme mode %q", ErrSourceUnsupported, mode.Mode)
		}
		byName := make(map[string]design.Token, len(theme.Tokens))
		for _, token := range theme.Tokens {
			if _, exists := byName[token.Name]; exists {
				return fmt.Errorf("%w: duplicate theme token %q", ErrSourceUnsupported, token.Name)
			}
			byName[token.Name] = token
		}
		for _, color := range mode.Colors {
			if byName[color.Name] != color {
				return fmt.Errorf("%w: mode %s colour %s differs from capture", ErrSourceUnsupported, mode.Mode, color.Name)
			}
		}
		for _, font := range mode.Fonts {
			token := byName[font.Name]
			families, err := design.ParseFontFamilies(token.Value)
			if err != nil || token.Type != "fontFamily" || !slices.Equal(families, font.Families) {
				return fmt.Errorf("%w: mode %s font %s differs from capture", ErrSourceUnsupported, mode.Mode, font.Name)
			}
		}
	}
	return nil
}
