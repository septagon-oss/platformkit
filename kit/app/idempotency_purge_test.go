package app

// The third kernel table that nothing but time makes smaller, and the job this
// composition schedules to empty it. `kit/httpx/idempotency_test.go` proves the
// delete itself, by calling `httpx.PurgeIdempotency`; nothing proved the line in
// `kernelJobs` until now, which is the difference between a purge that exists and
// a purge that runs. An installation that takes the kernel's idempotent commands
// and no module of its own has nobody else to schedule it — the same argument
// `jobs_test.go` makes for the counter table, one table later.

import (
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// TestTheRunnerPurgesTheIdempotencyTableItWrites runs the job off the list the
// worker is built from, over the real table: the answer whose day is over goes,
// the one still inside its window stays. Both rows are written as the superuser,
// because the table is system-only and the purge is what the superuser job is —
// a case that could not see the table could say nothing about it.
func TestTheRunnerPurgesTheIdempotencyTableItWrites(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	purge := kernelJob(t, kernelJobs(memory.New(), ""), "idempotency-purge")

	tenant, actor := uuid.New(), uuid.New()
	claim := func(key string, expiresIn string) {
		t.Helper()
		_, err := admin.ExecContext(t.Context(), `INSERT INTO platformkit_idempotency
			(tenant_id, actor_id, operation, key, request_hash, claimed_at, settled, status,
			 response, expires_at)
			VALUES ($1,$2,'POST /notes',$3,'\x01'::bytea, now(), true, 200, '{"id":1}'::bytea,
				now() + interval '`+expiresIn+`')`, tenant, actor, key)
		if err != nil {
			t.Fatalf("claim %s: %v", key, err)
		}
	}
	claim("expired", "-1 minute")
	claim("open", "1 hour")

	// Both rows are there before the job runs, or the count after it would be a
	// statement about a table somebody never filled.
	rows := func() int {
		t.Helper()
		var n int
		if err := admin.QueryRowContext(t.Context(),
			`SELECT count(*) FROM platformkit_idempotency WHERE tenant_id = $1`, tenant).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if before := rows(); before != 2 {
		t.Fatalf("the table holds %d rows before the job, want the spent answer and the live one", before)
	}
	if err := purge.Run(t.Context(), conn); err != nil {
		t.Fatalf("run %s: %v", purge.Name, err)
	}
	if got := rows(); got != 1 {
		t.Errorf("after %s the table holds %d rows, want the one still inside its window; the promise is that a "+
			"day is a day and the purge is what keeps it", purge.Name, got)
	}
	if err := purge.Run(t.Context(), conn); err != nil {
		t.Fatalf("run %s again: %v", purge.Name, err)
	}
	if got := rows(); got != 1 {
		t.Errorf("a second pass left %d rows, want 1: the live answer is not a candidate twice", got)
	}
}
