package migrations_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/migrations"
)

func TestRerunningMigrationsPreservesASettledCommandAnswer(t *testing.T) {
	adminURL, _ := dbtest.URLs(t)
	if err := db.Migrate(t.Context(), adminURL, migrations.Source); err != nil {
		t.Fatal(err)
	}
	admin := dbtest.Open(t, adminURL)
	key := uuid.NewString()
	_, err := admin.ExecContext(t.Context(), `INSERT INTO platformkit_idempotency
		(tenant_id, actor_id, operation, key, request_hash, claimed_at, expires_at,
		 settled, status, content_type, response)
		VALUES ($1, $2, 'POST /commands', $3, $4, now(), now() + interval '24 hours',
		 true, 201, 'application/json', $5)`, uuid.New(), uuid.New(), key,
		[]byte("original request hash"), []byte(`{"done":true}`))
	if err != nil {
		t.Fatal(err)
	}
	var before string
	if err := admin.QueryRowContext(t.Context(),
		`SELECT row_to_json(c)::text FROM platformkit_idempotency c WHERE key = $1`, key).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(t.Context(), adminURL, migrations.Source); err != nil {
		t.Fatalf("restart migration: %v", err)
	}
	var after string
	if err := admin.QueryRowContext(t.Context(),
		`SELECT row_to_json(c)::text FROM platformkit_idempotency c WHERE key = $1`, key).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("restart changed the answer held for a retry:\nbefore %s\nafter %s", before, after)
	}
}
