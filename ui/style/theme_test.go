package style_test

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui/style"
)

// These cases moved here with ThemeVars: they read the rendered stylesheet, so
// they belong to the package that renders it, and design stays a leaf with no
// stylesheet to test. hex is the only colour form a token may take.
var hex = regexp.MustCompile(`^#[0-9a-f]{6}$`)

func TestEveryThemeSetsEveryColourAsHex(t *testing.T) {
	t.Parallel()
	for _, theme := range design.Default().Both() {
		css := style.ThemeVars(design.Light(), design.Dark()).CSS()
		if !strings.Contains(css, theme.SurfacePrimary) {
			t.Errorf("theme %q is not in the rendered stylesheet", theme.Name)
		}
		for _, decl := range strings.Split(css, "\n") {
			name, value, ok := strings.Cut(strings.TrimSpace(decl), ": ")
			if !ok || !strings.HasPrefix(name, "--pk-color-") {
				continue
			}
			if !hex.MatchString(strings.TrimSuffix(value, ";")) {
				t.Errorf("%s is %q, which is not a six-digit hex colour", name, value)
			}
		}
	}
}

func TestBothThemesDeclareTheSameProperties(t *testing.T) {
	t.Parallel()
	css := style.ThemeVars(design.Light(), design.Dark()).CSS()
	root, dark := properties(css, ":root {"), properties(css, `[data-theme="dark"] {`)
	if len(root) == 0 || len(dark) == 0 {
		t.Fatalf("found %d light and %d dark properties", len(root), len(dark))
	}
	for name := range root {
		if strings.HasPrefix(name, "--pk-color-") && !dark[name] {
			t.Errorf("the dark theme does not set %s, so it inherits a light colour", name)
		}
	}
	for name := range dark {
		if !root[name] {
			t.Errorf("the dark theme sets %s, which the light theme does not define", name)
		}
	}
}

func TestTheAttributeBeatsThePreference(t *testing.T) {
	t.Parallel()
	css := style.ThemeVars(design.Light(), design.Dark()).CSS()
	attribute := strings.Index(css, `[data-theme="dark"] {`)
	query := strings.Index(css, "@media (prefers-color-scheme: dark)")
	if attribute < 0 || query < 0 {
		t.Fatal("the stylesheet has no dark theme")
	}
	if !strings.Contains(css[query:], ":root:not([data-theme])") {
		t.Error("the preference query is not qualified, so it would beat an explicit choice")
	}
}

func properties(css, header string) map[string]bool {
	i := strings.Index(css, header)
	if i < 0 {
		return nil
	}
	block := css[i+len(header):]
	block = block[:strings.Index(block, "\n}")]
	out := map[string]bool{}
	for _, line := range strings.Split(block, "\n") {
		if name, _, ok := strings.Cut(strings.TrimSpace(line), ": "); ok {
			out[name] = true
		}
	}
	return out
}

// The export and the stylesheet are two readings of one list. Every exported
// token is declared with its value; each colour is declared three times (on
// :root, under [data-theme="dark"] and again under the media query) and each
// font stack once, because the dark theme only overrides colours.
func TestExportedTokensAreTheDeclarationsTheStylesheetEmits(t *testing.T) {
	t.Parallel()
	light, dark := design.Light(), design.Dark()
	css := style.ThemeVars(light, dark).CSS()
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
	if got := strings.Count(css, "--pk-"); got != 3*25+3 {
		t.Errorf("the stylesheet declares %d custom properties; the export has 22 colours, 3 shapes and 3 font stacks", got)
	}
}

func TestExportedTokensFollowOverridesAndRemainDetached(t *testing.T) {
	t.Parallel()
	light, dark := design.Light(), design.Dark()
	light.AccentDefault, dark.AccentDefault = "#123456", "#abcdef"
	css := style.ThemeVars(light, dark).CSS()
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

func TestTypographyOverridesUseTheExistingTokensAndThemeCascade(t *testing.T) {
	t.Parallel()
	baseline := design.Default()
	theme := baseline
	theme.Light.Typography = design.Typography{Display: `"Example Display", serif`, Mono: `"Example Mono", monospace`}
	theme.Dark.Typography = theme.Light.Typography
	for _, palette := range theme.Both() {
		want := []design.Token{
			{Name: "--pk-font-display", Type: "fontFamily", Value: `"Example Display", serif`},
			{Name: "--pk-font-body", Type: "fontFamily", Value: design.FontBody},
			{Name: "--pk-font-mono", Type: "fontFamily", Value: `"Example Mono", monospace`},
		}
		tokens := palette.Tokens()
		if !slices.Equal(tokens[22:25], want) {
			t.Fatalf("typography override or per-role fallback lost: %+v", tokens[22:25])
		}
		tokens[22].Value = "not a mutation of the theme"
		if palette.Tokens()[22].Value != want[0].Value {
			t.Fatal("exported typography is not detached")
		}
	}
	shared := style.ThemeVars(theme.Light, theme.Dark).CSS()
	if strings.Count(shared, "--pk-font-display:") != 1 {
		t.Fatal("identical typography must be inherited from the root")
	}
	theme.Dark.Typography.Body = `"Accessible Body", sans-serif`
	changed := style.ThemeVars(theme.Light, theme.Dark).CSS()
	for _, selector := range []string{`[data-theme="dark"] {`, `:root:not([data-theme]) {`} {
		start := strings.Index(changed, selector)
		if start < 0 || !properties(changed, selector)["--pk-font-body"] {
			t.Fatalf("dark typography is absent from %s", selector)
		}
		block, _, _ := strings.Cut(changed[start:], "}")
		if !strings.Contains(block, `--pk-font-body: "Accessible Body", sans-serif;`) {
			t.Fatalf("dark typography disagrees with its exported token: %s", block)
		}
	}
	if design.Default() != baseline {
		t.Fatal("typography configuration changed defaults or lost comparable value semantics")
	}
}
