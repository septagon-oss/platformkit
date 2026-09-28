package designconfig

import (
	"os"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
)

// The loader is where design.yaml becomes the Pair ui.Compose receives, so it is
// where a file that would put an unreadable client on screen has to stop.
func TestLoadClientDesignReadsTheSeedAndItsOverrides(t *testing.T) {
	t.Parallel()
	client, err := LoadClientDesign(os.DirFS("testdata/clients"), "pets")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if client.Seed.Brand != "#f0b978" || client.Slug != "pets" {
		t.Errorf("loaded %+v", client)
	}
	pair, err := client.Resolve()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if pair.Light.AccentDefault != "#7a4a12" || pair.Dark.AccentDefault != "#e0a35c" {
		t.Errorf("override did not reach the pair: light %q dark %q", pair.Light.AccentDefault, pair.Dark.AccentDefault)
	}
	if pair.Light.Typography.Display != "Iowan Old Style, Georgia, serif" {
		t.Errorf("type did not reach the pair: %q", pair.Light.Typography.Display)
	}
	// The generated pair carries the client's own identity: an amber accent, not
	// the reference palette's green.
	if pair.Dark.AccentDefault == design.Default().Dark.AccentDefault {
		t.Errorf("generated pair kept the shipped accent")
	}
}

// make check refuses a design.yaml override below 4.5:1. This is that check: the
// file above is committed, it is wrong, and the loader says so with the pair and
// the ratio it measured.
func TestLoadClientDesignRefusesAnOverrideBelowContrast(t *testing.T) {
	t.Parallel()
	client, err := LoadClientDesign(os.DirFS("testdata/clients"), "broken")
	if err != nil {
		t.Fatalf("validate is the resolve step's job: %v", err)
	}
	pair, err := client.Resolve()
	if err == nil {
		t.Fatalf("an override below the gate resolved to %+v", pair.Light.TextMuted)
	}
	if pair != (design.Pair{}) {
		t.Errorf("a refused client returned a pair anyway")
	}
	if !strings.Contains(err.Error(), "text-muted") || !strings.Contains(err.Error(), "4.5:1") {
		t.Errorf("refusal does not name the token and the floor: %v", err)
	}
}

func TestLoadClientDesignRefusesFilesThatAreNotATrustworthyIdentity(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ slug, want string }{
		{"copied", `names slug "someone-else"`},
		{"stray", "which no theme exports"},
	} {
		_, err := LoadClientDesign(os.DirFS("testdata/clients"), tc.slug)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("client %s: got %v, want it to say %q", tc.slug, err, tc.want)
		}
	}
	_, err := LoadClientDesign(os.DirFS("testdata/clients"), "absent")
	if err == nil {
		t.Errorf("a missing design.yaml loaded")
	}
}

// One process, many clients: the set is keyed by slug, and two clients whose
// palettes a reader would take for one another never both get in.
func TestLoadClientDesignsKeysPairsBySlug(t *testing.T) {
	t.Parallel()
	pairs, err := LoadClientDesigns(os.DirFS("testdata/set"))
	if err != nil {
		t.Fatalf("load set: %v", err)
	}
	if len(pairs) != 2 {
		t.Fatalf("loaded %d clients, want alpha and beta", len(pairs))
	}
	for _, slug := range []string{"alpha", "beta"} {
		pair, ok := pairs[slug]
		if !ok {
			t.Fatalf("no pair for %s", slug)
		}
		if err := pair.Check(); err != nil {
			t.Errorf("%s: %v", slug, err)
		}
	}
	if got := design.Distance(pairs["alpha"], pairs["beta"]); design.Colliding(pairs["alpha"], pairs["beta"]) {
		t.Errorf("two clients sit %.3f apart, below the %.2f the register enforces", got, design.MinDistance)
	}
}
