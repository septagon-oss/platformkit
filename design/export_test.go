package design_test

import (
	"regexp"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/design"
)

// hex is the only colour form a token may take. A named colour or an rgb()
// would render, and it would also make the two themes impossible to compare.
var hex = regexp.MustCompile(`^#[0-9a-f]{6}$`)

func TestExportedTokenIdentitiesAndTypes(t *testing.T) {
	t.Parallel()
	colors := []string{
		"surface-canvas", "surface-primary", "surface-muted", "text-primary", "text-muted",
		"border-default", "border-strong", "accent-default", "accent-hover", "accent-on", "focus",
		"status-ok", "status-okbg", "status-warning", "status-warningbg", "status-danger",
		"status-dangerbg", "status-info", "status-infobg", "sidebar-bg", "sidebar-text", "sidebar-muted",
	}
	for _, theme := range design.Default().Both() {
		tokens := theme.Tokens()
		if len(tokens) != 28 {
			t.Fatalf("%s exports %d tokens, want 22 colors, 3 shapes and 3 font stacks", theme.Name, len(tokens))
		}
		for i, name := range colors {
			if token := tokens[i]; token.Name != "--pk-color-"+name || token.Type != "color" || !hex.MatchString(token.Value) {
				t.Errorf("%s token %d: %+v", theme.Name, i, token)
			}
		}
		wantFonts := []design.Token{
			{Name: "--pk-font-display", Type: "fontFamily", Value: `"Iowan Old Style", "Palatino Linotype", Palatino, Georgia, serif`},
			{Name: "--pk-font-body", Type: "fontFamily", Value: `"IBM Plex Sans", Aptos, "Helvetica Neue", sans-serif`},
			{Name: "--pk-font-mono", Type: "fontFamily", Value: `"IBM Plex Mono", "SFMono-Regular", Consolas, monospace`},
		}
		if !slices.Equal(tokens[22:25], wantFonts) {
			t.Errorf("%s changed its exported font stacks: %+v", theme.Name, tokens[22:25])
		}
	}
}
