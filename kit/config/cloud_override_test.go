package config_test

// What a profile stops demanding.
//
// A profile answers the seams a deployment left open, and an override is a
// deployment that did not leave one open. The keys a profile demands belong to the
// adapter it would have written, so a seam the deployment answered with another
// adapter leaves the profile with nothing to ask: a store an installation chose not
// to run needs no endpoint, and a cache it chose to keep in the process needs no
// address. What the deployment still runs on — its mail host, its collector — stays
// demanded whatever it swapped elsewhere, because those keys belong to no seam it
// could swap.

import (
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
)

func TestAProfileDemandsNothingForTheSeamAnOverrideAnswers(t *testing.T) {
	for _, tc := range []struct {
		what, body string
		silent     []string
		still      []string
	}{
		{
			what:   "the disk under a profile that would have named an object store",
			body:   "files:\n  store: \"local-disk\"\n",
			silent: []string{"files.s3.endpoint", "files.s3.bucket", "files.s3.region", "files.s3.access_key", "files.s3.secret_key"},
			still:  []string{"cache.url", "cache.app", "mail.host", "mail.from", "telemetry.otlp_endpoint"},
		},
		{
			what:   "an in-process cache under a profile that would have named valkey",
			body:   "cache:\n  adapter: \"memory\"\n",
			silent: []string{"cache.url", "cache.app"},
			still:  []string{"files.s3.endpoint", "files.s3.bucket", "files.s3.region", "mail.host", "telemetry.otlp_endpoint"},
		},
		// The control: the same profile over a deployment that wrote nothing asks
		// every one of its keys, so the silence above is the override and not a cell
		// that stopped asking anybody anything.
		{
			what:  "nothing anywhere",
			body:  "",
			still: []string{"files.s3.endpoint", "files.s3.bucket", "files.s3.region", "cache.url", "cache.app", "mail.host", "telemetry.otlp_endpoint"},
		},
	} {
		t.Run(tc.what, func(t *testing.T) {
			cfg := boots(t, cloudOf(config.ProfileAWS, tc.body))
			unmet := cfg.CloudUnmet(config.ProfileAWS)
			for _, key := range tc.silent {
				if slices.Contains(unmet, key) {
					t.Errorf("the profile demands %s for a seam the deployment answered itself: %v", key, unmet)
				}
			}
			for _, key := range tc.still {
				if !slices.Contains(unmet, key) {
					t.Errorf("the profile stopped demanding %s, which the override left untouched: %v", key, unmet)
				}
			}
		})
	}
}
