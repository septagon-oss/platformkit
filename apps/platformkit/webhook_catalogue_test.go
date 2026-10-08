package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/pkit"
)

// TestTheWebhookCatalogueNamesWhatTheCompositionEmits is the drift case the port's
// own doc promises: the composition writes which event names may be subscribed to,
// and a case asks the resolved composition whether the list it wrote is the set that
// composition can emit.
//
// Both directions are the test. A name the literal carries that nothing emits is a
// picker option that never fires — a subscriber that waits forever for an event no
// module produces. A name some composed module emits that the literal omits is a
// subscription this application refuses to register for something it does emit, and
// the refusal would name an event its own outbox is holding.
//
// It is a failing test rather than a derivation because the list must be written
// down: the modules that emit are composed by this application, and an
// `EventTypes()` that asked them at run time would be a second composition hidden
// inside a port.
func TestTheWebhookCatalogueNamesWhatTheCompositionEmits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(compositionConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	r := composeReference(cfg, pkit.Development)

	var emitted []string
	for _, m := range r.modules {
		for _, d := range m.Declared {
			emitted = append(emitted, d.Name)
		}
	}
	slices.Sort(emitted)
	emitted = slices.Compact(emitted)

	written := append([]string(nil), webhookCatalogue...)
	slices.Sort(written)
	if got := slices.Compact(written); len(got) != len(written) {
		var doubled []string
		for i := 1; i < len(written); i++ {
			if written[i] == written[i-1] {
				doubled = append(doubled, written[i])
			}
		}
		t.Fatalf("the catalogue names an event twice: %v", doubled)
	}
	if missing := notIn(emitted, written); len(missing) > 0 {
		t.Errorf("the composition emits %d event(s) the webhook catalogue does not offer: %v", len(missing), missing)
	}
	if extra := notIn(written, emitted); len(extra) > 0 {
		t.Errorf("the webhook catalogue offers %d event(s) this composition cannot emit: %v", len(extra), extra)
	}
}

// notIn is the names in `from` that `into` does not carry. Both are sorted, so
// slices.ContainsFunc over a sorted list is the whole algorithm and no map is
// built to answer a question about two handfuls of strings.
func notIn(from, into []string) []string {
	var out []string
	for _, name := range from {
		if !slices.Contains(into, name) {
			out = append(out, name)
		}
	}
	return out
}
