package migrations_test

import (
	"io/fs"
	"os"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func appliedModuleSources(t *testing.T, owner, head, historical string) []db.MigrationSource {
	t.Helper()
	previous := slices.Clone(everything)
	for i, source := range previous {
		if source.Owner != owner {
			continue
		}
		files := fstest.MapFS{}
		names, err := fs.Glob(source.Files, "*.up.sql")
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range names {
			if name[:6] > head {
				continue
			}
			body, err := fs.ReadFile(source.Files, name)
			if err != nil {
				t.Fatal(err)
			}
			files[name] = &fstest.MapFile{Data: body}
		}
		body, err := os.ReadFile("testdata/applied_module_history/" + historical)
		if err != nil {
			t.Fatal(err)
		}
		files[historical] = &fstest.MapFile{Data: body}
		previous[i].Files = files
	}
	return previous
}

func TestAnInstallationWithPasskeysKeepsItsAppliedMigrationHistory(t *testing.T) {
	previous := appliedModuleSources(t, "auth", "000047", "000035_passkeys.up.sql")
	adminURL, _ := dbtest.URLs(t)
	if err := db.Migrate(t.Context(), adminURL, previous...); err != nil {
		t.Fatalf("install the previously numbered passkey migration: %v", err)
	}
	admin := dbtest.Open(t, adminURL)
	tenant := uuid.New()
	if _, err := admin.ExecContext(t.Context(),
		"INSERT INTO passkey_settings (tenant_id, sign_in) VALUES ($1, true)", tenant); err != nil {
		t.Fatal(err)
	}
	var applied string
	if err := admin.QueryRowContext(t.Context(),
		"SELECT applied_at::text FROM schema_migrations WHERE owner = 'auth' AND version = 35").Scan(&applied); err != nil {
		t.Fatalf("reach an installation that applied the previous passkey version: %v", err)
	}
	if err := db.Migrate(t.Context(), adminURL, everything...); err != nil {
		t.Fatalf("upgrade must preserve an installation that already applied passkeys: %v", err)
	}
	var retained bool
	if err := admin.QueryRowContext(t.Context(),
		"SELECT sign_in FROM passkey_settings WHERE tenant_id = $1", tenant).Scan(&retained); err != nil || !retained {
		t.Errorf("upgrade lost the tenant's passkey setting: sign_in=%t, error=%v", retained, err)
	}
	var after string
	if err := admin.QueryRowContext(t.Context(),
		"SELECT applied_at::text FROM schema_migrations WHERE owner = 'auth' AND version = 35").Scan(&after); err != nil || after != applied {
		t.Errorf("upgrade replaced applied migration history: before=%q after=%q, error=%v", applied, after, err)
	}
}
