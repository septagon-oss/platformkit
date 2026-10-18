package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// TestMain gives this package a database of its own and one installation inside it.
//
// Every case here installs an application — the whole ledger, a tenant and an administrator —
// because that is the claim the package exists to prove: the README's third command run exactly as a
// person runs it. A migration run holds one advisory lock for its whole composition, an advisory lock
// belongs to a database, and `go test ./...` runs every other package that migrates against the
// database the test URLs name. Without the private database the package's own migrations queue behind
// every other package's, and a whole-suite run reaches the bound its `go test` carries while it is
// still working — six passing cases then reported as `(unknown)` over a dump of tests parked in the
// lock. The same reasoning, the same remedy and the same owner are `kit/db`'s TestMain.
//
// The one installation is the same argument one level in: two hundred cases that install the same
// composition were paying the migration and the bootstrap each time, in their own schema, queued at
// the same lock. Preparing it once and copying it per case (`dbtest.Clone`, taken by configure) is
// what leaves this package's wall to its own assertions. The cases for which installing is the thing
// under test still install for themselves; see install.
func TestMain(m *testing.M) {
	os.Exit(dbtest.RunInOwnDatabaseWithTemplate(m, prepareInstallation))
}

// prepareInstallation installs the composition once, into the namespace dbtest prepared for the
// template, through the same command a person runs: the bootstrap of this file's own install, with
// the same answers and the same configuration file. What it writes is what every case that takes a
// copy starts from, so it prepares exactly what one of them would have prepared for itself.
func prepareInstallation(adminURL, appURL string) error {
	dir, err := os.MkdirTemp("", "platformkit-installation")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	path := filepath.Join(dir, "config.yaml")
	body := "server:\n  addr: \"127.0.0.1:0\"\n  public_host: \"platformkit.localhost\"\n  installation_host: \"" + acmeHost + "\"\n  docs: true\n" +
		"database:\n  url: \"" + appURL + "\"\n  migrate_url: \"" + adminURL + "\"\n  lock_timeout: \"" + fixtureLockTimeout.String() + "\"\n" +
		"nats:\n  url: \"nats://localhost:4222\"\n" +
		"log:\n  level: \"error\"\n" +
		"files:\n  dir: \"" + filepath.Join(dir, "files") + "\"\n" +
		"webhook:\n  secret_key: \"" + webhookKey + "\"\n" +
		"auth:\n  oidc:\n    issuer: \"\"\n    client_id: \"\"\n    client_secret: \"\"\n    redirect_path: \"\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return err
	}
	if _, err := config.Load(path); err != nil {
		return err
	}
	// The one secret bootstrap takes arrives through the environment, the way every deployment
	// hands it over. This runs before any case does, so the process's own value is the honest place.
	if err := os.Setenv("PLATFORMKIT_BOOTSTRAP_PASSWORD", adminPass); err != nil {
		return err
	}
	return bootstrap(installationArgs(path))
}
