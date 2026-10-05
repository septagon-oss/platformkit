package main

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
)

// TestTheReferenceCompositionNamesItsAppOnlyThroughTheSetting holds the reference
// application to one answer for "which app am I": it names itself through nats.app
// alone and leaves Options.App empty, so New, the `platformkit migrate` door
// (app.Migrate reads the configuration, not the options) and the tenant module's
// stamp (appSlug) all read the same key. A composition that started naming itself
// in code would place tenants from the migrate door under a different app than its
// boot relays for.
func TestTheReferenceCompositionNamesItsAppOnlyThroughTheSetting(t *testing.T) {
	for _, slug := range []string{"", "collect"} {
		t.Run("nats.app="+slug, func(t *testing.T) {
			_, cfg := configure(t)
			cfg.NATS.App = slug
			opts := appOptions(cfg, compose(cfg), app.All)
			if opts.App.Named() {
				t.Fatalf("the reference composition names its app %q in code; the migrate door and the tenant stamp read nats.app %q", opts.App, slug)
			}
			if got := appSlug(cfg).String(); got != slug {
				t.Fatalf("the tenant stamp names app %q, want the setting's %q", got, slug)
			}
		})
	}
}
