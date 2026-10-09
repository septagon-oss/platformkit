package main

// The composition file is decision 0074 rule 4's deliverable: what the reference
// application resolves to, in text, committed, so a reader can diff what the
// application is without running it. It is written by pkit.Server.Explain — the
// resolved composition, the module that provides each port the kernel asks the
// application for, the tenant hosts the process claims — and never by hand.
//
// UPDATE_GOLDEN=1 writes the file; without it, a file that disagrees with the
// composition is a failure. That is the whole reason it is committed: a review
// can read the file, and a change to the composition that nobody described in
// words fails a check rather than passing quietly.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/pkit"
)

// compositionConfig is the installation the composition file describes: the
// reference deployment's own hosts, written down here rather than read from a
// temporary directory, because a file that is committed cannot be worded around
// a path that changes on every run. It needs no database: Explain resolves a
// composition and answers about it, and opens nothing.
const compositionConfig = `server:
  addr: ":8080"
  public_host: "platformkit.localhost"
  installation_host: "installation.platformkit.localhost"
  docs: true
database:
  url: "postgres://platformkit_app:platformkit@localhost:5432/platformkit?sslmode=disable"
  migrate_url: "postgres://postgres:platformkit@localhost:5432/platformkit?sslmode=disable"
nats:
  url: "nats://localhost:4222"
log:
  level: "info"
files:
  dir: "data/files"
auth:
  oidc:
    issuer: ""
    client_id: ""
    client_secret: ""
    redirect_path: ""
`

func TestCompositionFile(t *testing.T) {
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
		name := "COMPOSITION." + string(env) + ".md"
		want := composeReference(cfg, env).explain(app.All)
		if write {
			if err := os.WriteFile(name, []byte(want), 0o644); err != nil {
				t.Fatalf("write %s: %v", name, err)
			}
			continue
		}
		got, err := os.ReadFile(name)
		if err != nil {
			t.Errorf("%s is not committed: %v", name, err)
			continue
		}
		if string(got) != want {
			t.Errorf("%s is not what the composition resolves to today; regenerate with UPDATE_GOLDEN=1 go test ./apps/platformkit -run TestCompositionFile\n--- committed ---\n%s\n--- resolved now ---\n%s",
				name, got, want)
		}
	}
}
