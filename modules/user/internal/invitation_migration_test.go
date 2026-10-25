package internal_test

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/migrations"
	auth "github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/user"
)

func TestInvitationAdoptionBatchesLegacyRowsWithoutInventingRequests(t *testing.T) {
	adminURL, _ := dbtest.URLs(t)
	admin := dbtest.Open(t, adminURL)
	prior := fstest.MapFS{}
	names, err := fs.Glob(user.Migrations.Files, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if name >= "000075" {
			continue
		}
		data, err := fs.ReadFile(user.Migrations.Files, name)
		if err != nil {
			t.Fatal(err)
		}
		prior[name] = &fstest.MapFile{Data: data}
	}
	if err := db.Migrate(t.Context(), adminURL, migrations.Source, db.MigrationSource{Owner: "user", Files: prior, Adopts: user.Migrations.Adopts, RulesFrom: user.Migrations.RulesFrom}); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(`INSERT INTO users (tenant_id,email,status)
 SELECT $1,'legacy'||n||'@example.com','invited' FROM generate_series(1,1002) n`, acme.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(`INSERT INTO users (tenant_id,email,status,deleted_at) VALUES
 ($1,'active@example.com','active',NULL),($1,'inactive@example.com','inactive',NULL),($1,'deleted@example.com','invited',now())`, acme.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(t.Context(), adminURL, migrations.Source, user.Migrations); err != nil {
		t.Fatal(err)
	}
	var adopted, untouched, events int
	var first string
	if err := admin.QueryRow(`SELECT count(*),min(invitation_adopted_at)::text FROM users
 WHERE invitation_phase='pending' AND invitation_generation=0 AND invitation_revision=1
 AND invitation_recipient=email AND invitation_requested_at IS NULL AND invitation_delivery_id IS NULL
 AND extract(epoch FROM invitation_expires_at-invitation_adopted_at)=$1`, auth.TokenLifetime.Seconds()).Scan(&adopted, &first); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow("SELECT count(*) FROM users WHERE invitation_phase IS NULL").Scan(&untouched); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow("SELECT count(*) FROM platformkit_outbox").Scan(&events); err != nil {
		t.Fatal(err)
	}
	if adopted != 1002 || untouched != 3 || events != 0 {
		t.Fatalf("adopted=%d unrelated=%d events=%d", adopted, untouched, events)
	}
	if err := db.Migrate(t.Context(), adminURL, migrations.Source, user.Migrations); err != nil {
		t.Fatal(err)
	}
	var repeated string
	if err := admin.QueryRow("SELECT min(invitation_adopted_at)::text FROM users").Scan(&repeated); err != nil {
		t.Fatal(err)
	}
	if repeated != first {
		t.Fatal("reapplying migration moved adoption deadline")
	}
}
