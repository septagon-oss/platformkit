package migrations_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestRerunningTheCompositionPreservesUnconfirmedCommandEvidence(t *testing.T) {
	adminURL, _ := dbtest.URLs(t)
	if err := db.Migrate(t.Context(), adminURL, everything...); err != nil {
		t.Fatal(err)
	}
	admin := dbtest.Open(t, adminURL)
	tenant, actor := uuid.New(), uuid.New()
	for _, committed := range []bool{false, true} {
		key, request := uuid.NewString(), uuid.NewString()
		_, err := admin.ExecContext(t.Context(), `INSERT INTO platformkit_idempotency
			(tenant_id, actor_id, operation, key, request_hash, claimed_at, expires_at,
			 settled, unconfirmed, status, content_type, response, request_id)
			VALUES ($1, $2, 'POST /commands', $3, $4, now(), now() + interval '24 hours',
			 true, true, 201, 'application/json', $5, $6)`, tenant, actor, key,
			[]byte("original request hash"), []byte(`{"done":true}`), request)
		if err != nil {
			t.Fatal(err)
		}
		if committed {
			_, err = admin.ExecContext(t.Context(), `INSERT INTO platformkit_command_effect
				(tenant_id, actor_id, operation, key, request_id)
				VALUES ($1, $2, 'POST /commands', $3, $4)`, tenant, actor, key, request)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	readEvidence := func() string {
		t.Helper()
		var evidence string
		err := admin.QueryRowContext(t.Context(), `SELECT jsonb_build_object(
			'claims', (SELECT jsonb_agg(to_jsonb(c) ORDER BY key) FROM platformkit_idempotency c),
			'effects', (SELECT jsonb_agg(to_jsonb(e) ORDER BY key) FROM platformkit_command_effect e)
		)::text`).Scan(&evidence)
		if err != nil {
			t.Fatal(err)
		}
		return evidence
	}
	before := readEvidence()
	if err := db.Migrate(t.Context(), adminURL, everything...); err != nil {
		t.Fatalf("restart the composed installation: %v", err)
	}
	if after := readEvidence(); after != before {
		t.Fatalf("restart changed evidence for an unknown commit:\nbefore %s\nafter %s", before, after)
	}
}
