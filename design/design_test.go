package design_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
)

// hex is the only colour form a token may take. A named colour or an rgb()
// would render, and it would also make the two themes impossible to compare.
var hex = regexp.MustCompile(`^#[0-9a-f]{6}$`)

func TestEveryThemeSetsEveryColourAsHex(t *testing.T) {
	t.Parallel()
	for _, theme := range design.Themes() {
		css := design.CSS().CSS()
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
	css := design.CSS().CSS()
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
	css := design.CSS().CSS()
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
