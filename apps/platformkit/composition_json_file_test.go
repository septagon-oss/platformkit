package main

// The machine-readable composition file is the same decision as the text one
// beside it, encoded for a reader that is not a person: the modules this
// application resolves to, the contract each one supplies to which other, what
// Choose settled, which implementation the environment picked and which
// configuration sections were read. It is written by pkit.App.Describe — the
// same resolved plan Explain renders as sentences — and refused on drift by the
// UPDATE_GOLDEN discipline the text file already uses, so a change to the
// composition that nobody described in words fails a check in either encoding.
//
// The second assertion is the reason the file may exist: a committed JSON
// document is a wider surface than a markdown file a reviewer reads, so nothing
// a deployment supplies belongs in it. The configuration below names the app
// role's DSN and the installation's host; neither appears in the bytes.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestCompositionJSONFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(compositionConfig), 0o600); err != nil {
		t.Fatalf("write the configuration: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load the configuration: %v", err)
	}
	write := os.Getenv("UPDATE_GOLDEN") != ""
	for _, env := range []pkit.Environment{pkit.Development, pkit.Production} {
		name := "COMPOSITION." + string(env) + ".json"
		ref := composeReference(cfg, env)
		want, err := ref.describe()
		if err != nil {
			t.Fatalf("%s: the reference composition does not describe itself: %v", name, err)
		}
		for _, secret := range []string{
			"postgres://platformkit_app:platformkit@localhost:5432",
			"postgres://postgres:platformkit@localhost:5432",
			"installation.platformkit.localhost",
			"platformkit.localhost",
			"nats://localhost:4222",
			"data/files",
		} {
			if strings.Contains(string(want), secret) {
				t.Errorf("%s carries %q, which is a value the deployment supplied and not a name the composition declares", name, secret)
			}
		}
		if write {
			if err := os.WriteFile(name, want, 0o644); err != nil {
				t.Fatalf("write %s: %v", name, err)
			}
			continue
		}
		got, err := os.ReadFile(name)
		if err != nil {
			t.Errorf("%s is not committed: %v", name, err)
			continue
		}
		if string(got) != string(want) {
			t.Errorf("%s is not what the composition resolves to today; regenerate with UPDATE_GOLDEN=1 go test ./apps/platformkit -run TestCompositionJSONFile\n--- committed ---\n%s\n--- resolved now ---\n%s",
				name, got, want)
		}
	}
}
