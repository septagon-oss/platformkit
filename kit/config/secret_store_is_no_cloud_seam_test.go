package config_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
)

// TestNoCloudProfileNamesTheSecretStore checks that the secret store sits beside
// the cloud profile and not under it: no profile writes or demands a secrets.* key,
// and a deployment that names a profile keeps exactly the store it wrote — or, when
// it wrote none, the process's own environment.
func TestNoCloudProfileNamesTheSecretStore(t *testing.T) {
	for _, profile := range config.Profiles() {
		for _, cell := range config.CloudProfile(profile) {
			for _, w := range cell.Sets {
				if strings.HasPrefix(w.Key, "secrets.") {
					t.Errorf("profile %s writes %s", profile, w.Key)
				}
			}
			for _, name := range cell.Required {
				if strings.HasPrefix(name, "secrets.") {
					t.Errorf("profile %s demands %s", profile, name)
				}
			}
		}
	}

	unnamed := boots(t, cloudOf(config.ProfileAWS, awsInputs))
	if unnamed.Files.Store != config.StoreS3 || unnamed.Cache.Adapter != "valkey" {
		t.Fatalf("the aws profile did not resolve its seams (files.store %q, cache.adapter %q)",
			unnamed.Files.Store, unnamed.Cache.Adapter)
	}
	if unnamed.Secrets != (config.Secrets{}) {
		t.Errorf("an aws deployment that named no secret store reads from %+v; it reads the environment", unnamed.Secrets)
	}

	named := boots(t, cloudOf(config.ProfileAWS,
		awsInputs+"secrets:\n  adapter: \"file\"\n  dir: \"/run/secrets/platformkit\"\n"))
	if want := (config.Secrets{Adapter: "file", Dir: "/run/secrets/platformkit"}); named.Secrets != want {
		t.Errorf("an aws deployment that named a mounted directory reads from %+v; it wrote %+v", named.Secrets, want)
	}
}
