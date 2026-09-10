package style_test

import (
	"encoding/json"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui/style"
)

func TestSemanticColorsProjectTheExistingRoleOwner(t *testing.T) {
	roles := style.RoleColors()
	names := make([]string, 0, len(roles))
	values := make(map[string]design.ColorValue)
	css := style.RoleVars().CSS()
	for _, role := range roles {
		names = append(names, role.Name)
		values[role.Name] = role.Value
		value, err := role.Value.CSS()
		if err != nil || !strings.Contains(css, role.Name+": "+value+";") {
			t.Fatalf("source role %s disagrees with emitted CSS: %v", role.Name, err)
		}
	}
	if !slices.IsSorted(names) || len(values) != len(style.AllColors())+2 || len(values) != len(roles) {
		t.Fatal("role projection must be complete, unique and deterministically ordered")
	}
	for name, reference := range map[string]string{
		"--pk-role-surface-brand":   "--pk-color-accent-default",
		"--pk-role-ring-focus":      "--pk-color-focus",
		"--pk-role-fg-on-brand":     "--pk-color-accent-on",
		"--pk-role-surface-inverse": "--pk-color-sidebar-bg",
	} {
		if values[name].Reference != reference || values[name].Literal != "" || values[name].Mix != nil {
			t.Errorf("%s lost its explicit alias to %s", name, reference)
		}
	}
	for _, tc := range []struct {
		role, first, second string
		percent             float64
	}{
		{"surface-brand-soft", "accent-default", "surface-primary", 12},
		{"surface-hover", "text-primary", "surface-primary", 4},
		{"surface-active", "text-primary", "surface-primary", 8},
		{"fg-secondary", "text-primary", "surface-primary", 78},
		{"fg-tertiary", "text-primary", "surface-primary", 60},
		{"fg-placeholder", "text-muted", "surface-primary", 70},
		{"fg-disabled", "text-muted", "surface-primary", 55},
		{"border-secondary", "border-default", "surface-primary", 60},
	} {
		mix := values["--pk-role-"+tc.role].Mix
		if mix == nil || mix.First.Reference != "--pk-color-"+tc.first || mix.Second.Reference != "--pk-color-"+tc.second || mix.FirstPercent != tc.percent {
			t.Errorf("%s lost its authored mix: %+v", tc.role, mix)
		}
	}
	overlay := values["--pk-role-surface-overlay"].Mix
	if overlay == nil || overlay.First.Reference != "--pk-color-sidebar-bg" || overlay.FirstPercent != 55 || overlay.Second.Literal != "transparent" {
		t.Fatal("overlay must mix the sidebar with transparent, not an opaque surface")
	}
	before, _ := json.Marshal(roles)
	overlay.FirstPercent = 1
	again, _ := json.Marshal(style.RoleColors())
	if string(before) != string(again) {
		t.Fatal("returned source roles share mutable state with a subsequent call")
	}
}

func TestSemanticColorsResolveBothModesAndCallerPalette(t *testing.T) {
	for _, tc := range []struct {
		theme design.Theme
		want  design.SRGBA
	}{
		{design.Light(), design.SRGBA{18.0 / 255, 32.0 / 255, 29.0 / 255, 0.55}},
		{design.Dark(), design.SRGBA{10.0 / 255, 18.0 / 255, 16.0 / 255, 0.55}},
	} {
		got, err := design.ResolveColors(tc.theme.Tokens(), style.RoleColors())
		if err != nil {
			t.Fatal(err)
		}
		for i, want := range tc.want {
			if math.Abs(got["--pk-role-surface-overlay"][i]-want) > 1e-12 {
				t.Errorf("%s overlay: %v, want %v", tc.theme.Name, got["--pk-role-surface-overlay"], tc.want)
			}
		}
	}
	theme := design.Light()
	theme.AccentDefault, theme.Focus = "#ff0000", "#ff0000"
	before, err := design.ResolveColors(theme.Tokens(), style.RoleColors())
	if err != nil || before["--pk-role-surface-brand"] != before["--pk-role-ring-focus"] {
		t.Fatalf("caller palette was not used: %v", err)
	}
	theme.Focus = "#0000ff"
	after, err := design.ResolveColors(theme.Tokens(), style.RoleColors())
	if err != nil || after["--pk-role-surface-brand"] != (design.SRGBA{1, 0, 0, 1}) || after["--pk-role-ring-focus"] != (design.SRGBA{0, 0, 1, 1}) {
		t.Fatalf("equal baseline values merged different role dependencies: %v", err)
	}
	theme.AccentDefault = "currentColor"
	if got, err := design.ResolveColors(theme.Tokens(), style.RoleColors()); err == nil || got != nil {
		t.Fatal("context-dependent caller colour must remain unsupported, not receive a guessed value")
	}
}
