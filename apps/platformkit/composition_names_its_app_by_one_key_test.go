package main

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/pkit"
)

// TestTheReferenceCompositionNamesItsAppOnlyThroughTheSetting holds the reference
// application to one answer for "which app am I": it names itself through nats.app
// alone and leaves Options.App empty, so New, the `platformkit migrate` door
// (app.Migrate reads the configuration, not the options) and the tenant module's
// stamp all read the same key. A composition that started naming itself in code
// would place tenants from the migrate door under a different app than its boot
// relays for.
//
// The stamp itself is modules/tenant's now (its build reads the section, and
// Explain says which section it read), so the second half of this case asks the
// resolved composition to name the reader rather than reaching for a helper this
// file no longer has: a second place that formed the slug would be a second
// "reads config.NATS" line, and the sentence is the thing under test.
func TestTheReferenceCompositionNamesItsAppOnlyThroughTheSetting(t *testing.T) {
	for _, slug := range []string{"", "collect"} {
		t.Run("nats.app="+slug, func(t *testing.T) {
			_, cfg := configure(t)
			cfg.NATS.App = slug
			r := composeReference(cfg, pkit.Development)
			if r.options.App.Named() {
				t.Fatalf("the reference composition names its app %q in code; the migrate door and the tenant stamp read nats.app %q", r.options.App, slug)
			}
			text, err := r.server(app.All).Explain()
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Count(text, "pkit: tenant.Module reads config.NATS."); got != 1 {
				t.Errorf("the composition answered %d readers of config.NATS, want the one the tenant module is:\n%s", got, text)
			}
		})
	}
}
