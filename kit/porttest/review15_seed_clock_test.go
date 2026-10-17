package porttest

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// This file is review 15's pin. README, "The fake's plumbing", says of Seed:
// "puts a row the way a create route would: an id, the clock's stamp and the
// entity's own Validate". The middle clause is the one nothing watched: with
// `base.CreatedAt = at` replaced by a fixed instant, kit/porttest and all three
// converted suites — fakes and real Postgres services alike — stayed green,
// because every Snapshot in the repository renders UpdatedAt and none renders
// CreatedAt. This file reads both stamps, so the clause is a claim a suite can
// lose no more.
//
// Both tests take the instant from the clock the fixture moved, never from
// NewFake's argument, so neither can pass by the constructor's value happening
// to match, and both say which field is wrong by name rather than by a
// difference a broken stamp produces.

// stampedRow is this file's own row: a name nobody else can rename out from
// under the pin, and no Validate, so a refusal has nothing to do with the stamp.
type stampedRow struct {
	entity.Base
	Label string
}

func (stampedRow) TableName() string { return "stamped_rows" }

// seedAt builds a fake whose clock was moved to `at` after NewFake ran, seeds
// one row through it, and answers the row as the store holds it.
func seedAt(t *testing.T, at time.Time) stampedRow {
	t.Helper()
	f := NewFake(time.Unix(1700000000, 0).UTC(), nil)
	f.Clock.Set(at)
	rows := NewStore(func(r stampedRow) uuid.UUID { return r.ID })
	ctx := tenancy.WithTenant(context.Background(), tenancy.Tenant{ID: uuid.New(), Slug: "review15"})
	id := Seed(ctx, f, rows, stampedRow{Label: "pinned"})
	got, err := rows.Get(ctx, id)
	if err != nil {
		t.Fatalf("the seeded row is not in the store: %v", err)
	}
	return got
}

// TestSeedStampsTheRowFromTheFakeClock reads the clause's two fields.
func TestSeedStampsTheRowFromTheFakeClock(t *testing.T) {
	at := time.Date(2027, 3, 4, 5, 6, 7, 0, time.UTC)
	got := seedAt(t, at)
	if !got.CreatedAt.Equal(at) {
		t.Errorf("CreatedAt reads %s; the fake's clock read %s, and Seed stamps a row from the clock, not from anywhere else", got.CreatedAt, at)
	}
	if !got.UpdatedAt.Equal(at) {
		t.Errorf("UpdatedAt reads %s; the fake's clock read %s", got.UpdatedAt, at)
	}
}

// TestSeedStampsFromTheFakeClockAndNotTheWallClock is the same claim from the
// other side: the clock here is set decades before this run, so a stamp that
// came from the wall clock rather than the fake's is old by nothing. The claim
// is asserted as a distance the fake's own instant forces, not as a difference a
// broken stamp produces.
func TestSeedStampsFromTheFakeClockAndNotTheWallClock(t *testing.T) {
	at := time.Date(1999, 1, 2, 3, 4, 5, 0, time.UTC)
	got := seedAt(t, at)
	if !got.CreatedAt.Equal(at) {
		t.Errorf("CreatedAt reads %s; the fake's clock read %s", got.CreatedAt, at)
	}
	if d := time.Since(got.CreatedAt); d < 24*time.Hour {
		t.Errorf("CreatedAt reads %s, only %s old; the fake's clock was set to %s, so this stamp came from something other than the clock the fixture owns, and a case that moves time by moving that clock would not move it", got.CreatedAt, d, at)
	}
	if got.CreatedAt.Format(time.RFC3339Nano) != "1999-01-02T03:04:05Z" {
		t.Errorf("CreatedAt renders %q; the fake's time is UTC, so it renders 1999-01-02T03:04:05Z", got.CreatedAt.Format(time.RFC3339Nano))
	}
}
