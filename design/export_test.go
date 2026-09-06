package design_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
)

// The export and the stylesheet are two readings of one list. Every exported
// token is declared with its value; each colour is declared three times (on
// :root, under [data-theme="dark"] and again under the media query) and each
// font stack once, because the dark theme only overrides colours.
func TestExportedTokensAreTheDeclarationsTheStylesheetEmits(t *testing.T) {
	t.Parallel()
	light, dark := design.Light(), design.Dark()
	css := design.CSS(light, dark).CSS()
	for _, theme := range []design.Theme{light, dark} {
		for _, token := range theme.Tokens() {
			if !strings.Contains(css, token.Name+": "+token.Value+";") {
				t.Errorf("%s exports %s = %q, which the stylesheet does not declare", theme.Name, token.Name, token.Value)
			}
			want := 3
			if token.Type == "fontFamily" {
				want = 1
			}
			if got := strings.Count(css, token.Name+":"); got != want {
				t.Errorf("%s is declared %d times, want %d", token.Name, got, want)
			}
		}
	}
	if got := strings.Count(css, "--pk-"); got != 3*22+3 {
		t.Errorf("the stylesheet declares %d custom properties; the export has 22 colours and 3 font stacks", got)
	}
}

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
		if len(tokens) != 25 {
			t.Fatalf("%s exports %d tokens, want 22 colors and 3 font stacks", theme.Name, len(tokens))
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
		if !slices.Equal(tokens[22:], wantFonts) {
			t.Errorf("%s changed its exported font stacks: %+v", theme.Name, tokens[22:])
		}
	}
}

func TestExportedTokensFollowOverridesAndRemainDetached(t *testing.T) {
	t.Parallel()
	light, dark := design.Light(), design.Dark()
	light.AccentDefault, dark.AccentDefault = "#123456", "#abcdef"
	css := design.CSS(light, dark).CSS()
	for _, theme := range []design.Theme{light, dark} {
		tokens := theme.Tokens()
		i := slices.IndexFunc(tokens, func(token design.Token) bool { return token.Name == "--pk-color-accent-default" })
		if i < 0 || tokens[i].Value != theme.AccentDefault {
			t.Fatalf("%s export ignored its palette override", theme.Name)
		}
		wantCount := 1
		if theme.Name == "dark" {
			wantCount = 2
		}
		if strings.Count(css, tokens[i].Name+": "+tokens[i].Value+";") != wantCount {
			t.Errorf("%s CSS disagrees with the exported override", theme.Name)
		}
		tokens[i].Value = "changed by consumer"
		if theme.Tokens()[i].Value != theme.AccentDefault {
			t.Error("an export mutation changed a subsequent export")
		}
	}
	encoded, err := json.Marshal(light.Tokens()[0])
	if err != nil || string(encoded) != `{"name":"--pk-color-surface-canvas","type":"color","value":"#f2efe7"}` {
		t.Fatalf("token JSON contract: %s, %v", encoded, err)
	}
}
