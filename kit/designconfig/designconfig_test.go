package designconfig

import (
	"maps"
	"os"
	"slices"
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

// The two refusals a file can carry that are not style: a foreground with alpha,
// which has no ratio until it is composited, and a font stack the kernel's own
// parser refuses. Both are decided here, at the file, rather than reaching a
// theme where the contrast gate would measure a translucent colour as an opaque
// one and report a legible client.
func TestLoadClientDesignRefusesAValueWithNoMeaningToMeasure(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ slug, want string }{
		{"veil", "carries alpha"},
		{"invisible", "carries alpha"},
		{"contextual", `family keyword "inherit" must be quoted`},
	} {
		_, err := LoadClientDesign(os.DirFS("testdata/clients"), tc.slug)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("client %s: got %v, want it to say %q", tc.slug, err, tc.want)
		}
	}
}

// A client's shape is written in the spelling the tags give it, and reaches both
// themes of the pair so the stylesheet and the exported document agree.
func TestLoadClientDesignReadsAShapeWrittenTheWayAPersonWritesIt(t *testing.T) {
	t.Parallel()
	client, err := LoadClientDesign(os.DirFS("testdata/clients"), "shaped")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if client.Shape.CardRadius != "0.75rem" || client.Shape.ButtonRadius != "2px" || client.Shape.ModalRadius != "1.5rem" {
		t.Fatalf("kebab-case shape did not decode: %+v", client.Shape)
	}
	pair, err := client.Resolve()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	for _, theme := range pair.Both() {
		tokens := map[string]string{}
		for _, token := range theme.Tokens() {
			tokens[token.Name] = token.Value
		}
		if tokens["--pk-radius-card"] != "0.75rem" || tokens["--pk-radius-button"] != "2px" || tokens["--pk-radius-modal"] != "1.5rem" {
			t.Errorf("%s shape tokens: %v", theme.Name, tokens)
		}
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

// The set loader's headline promise, in the negative: two clients generated from
// one seed are one palette worn twice, and the second one to arrive is refused by
// name with the distance measured. Nothing partial comes back, so a process
// cannot boot half a client roster.
func TestLoadClientDesignsRefusesTwoClientsWearingOneIdentity(t *testing.T) {
	t.Parallel()
	pairs, err := LoadClientDesigns(os.DirFS("testdata/collide"))
	if err == nil {
		t.Fatalf("two clients on one seed loaded: %v", slices.Sorted(maps.Keys(pairs)))
	}
	if pairs != nil {
		t.Errorf("a refused set returned %d pairs", len(pairs))
	}
	message := err.Error()
	for _, want := range []string{"client b", "client a", "0.000", "below the 0.01"} {
		if !strings.Contains(message, want) {
			t.Errorf("refusal does not name %q: %v", want, message)
		}
	}
}

// And the brief's refusal of a third: two sound clients and one whose own
// override breaks its own body role. The set is refused whole — the message names
// the client that broke the gate, and the two that were fine do not boot without
// it, because a roster that silently loses a client is worse than no roster.
func TestLoadClientDesignsRefusesAThirdClientThatDoesNotClearTheGate(t *testing.T) {
	t.Parallel()
	pairs, err := LoadClientDesigns(os.DirFS("testdata/third"))
	if err == nil {
		t.Fatalf("a set holding an unreadable client loaded: %v", slices.Sorted(maps.Keys(pairs)))
	}
	if pairs != nil {
		t.Errorf("a refused set returned %d pairs", len(pairs))
	}
	if !strings.Contains(err.Error(), "client broken") || !strings.Contains(err.Error(), "text-muted") {
		t.Errorf("refusal does not name the client and its token: %v", err)
	}
}
