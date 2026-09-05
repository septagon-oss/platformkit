package design_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
)

func TestTokenExportPreservesExistingCSS(t *testing.T) {
	t.Parallel()
	const before = "7572b45da15e196c80da6f172007792b655a7a5906eb79de951981c7265fe9b8"
	got := fmt.Sprintf("%x", sha256.Sum256([]byte(design.CSS(design.Light(), design.Dark()).CSS())))
	if got != before {
		t.Fatalf("token export changed the existing stylesheet: hash %s", got)
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
