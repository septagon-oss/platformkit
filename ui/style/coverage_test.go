package style

import "testing"

// TestBgTotalOverAllColors locks the invariant that every semantic
// Color yields a non-empty "bg-<name>" class. A previous partial
// switch silently dropped colours like FgDisabled / FgSuccess etc.,
// which caused badge dot indicators to render with no background.
// This test iterates AllColors() and would fail on any future
// regression that drops a color from the Bg() path.
func TestBgTotalOverAllColors(t *testing.T) {
	for _, c := range AllColors() {
		got := classBg(c)
		if got == "" {
			t.Errorf("classBg(%q) returned empty — Bg() is not total over Color", c)
		}
	}
}

// TestTextTotalOverAllColors is the text-color counterpart.
func TestTextTotalOverAllColors(t *testing.T) {
	for _, c := range AllColors() {
		got := classText(c)
		if got == "" {
			t.Errorf("classText(%q) returned empty", c)
		}
	}
}

// TestBadgeDotDefaultColorBackground proves the reported regression
// (default-variant badge dots rendering without a background class)
// is fixed. style.Bg(FgDisabled).Compile() MUST emit "bg-fg-disabled".
func TestBadgeDotDefaultColorBackground(t *testing.T) {
	got := New().Bg(FgDisabled).Compile()
	want := "bg-fg-disabled"
	if got != want {
		t.Fatalf("default-variant dot fallback: got %q want %q", got, want)
	}
}
