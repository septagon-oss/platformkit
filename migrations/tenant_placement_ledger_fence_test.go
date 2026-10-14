package migrations_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestTenantPlacementHoldsItsLedgerFenceUntilCommit(t *testing.T) {
	owner, _ := dbtest.Schema(t)
	ctx := t.Context()
	id := uuid.New()
	if _, err := owner.ExecContext(ctx,
		`INSERT INTO tenants (id, slug, name, app) VALUES ($1, 'shop', 'Shop', '')`, id); err != nil {
		t.Fatal(err)
	}
	placement, err := owner.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer placement.Rollback()
	var unplaced string
	if err := placement.QueryRowContext(ctx,
		`SELECT platformkit_place_tenants('collect', '', 'shop=collect', NULL)`).Scan(&unplaced); err != nil {
		t.Fatal(err)
	}
	var placed string
	if err := placement.QueryRowContext(ctx, `SELECT app FROM tenants WHERE id=$1`, id).Scan(&placed); err != nil {
		t.Fatal(err)
	}
	if placed != "collect" || unplaced != "" {
		t.Fatalf("placement reached app=%q, unplaced=%q; want collect and no unplaced tenant", placed, unplaced)
	}
	move, err := owner.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer move.Rollback()
	const lock = `SELECT pg_try_advisory_xact_lock(hashtextextended('events ledger tenant ' || $1::text, 0))`
	var acquired bool
	if err := move.QueryRowContext(ctx, lock, id).Scan(&acquired); err != nil {
		t.Fatal(err)
	}
	if acquired {
		t.Fatal("a ledger move acquired the tenant's exclusive fence before placement committed")
	}
	if err := placement.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := move.QueryRowContext(ctx, lock, id).Scan(&acquired); err != nil {
		t.Fatal(err)
	}
	if !acquired {
		t.Fatal("placement retained its tenant's ledger fence after commit")
	}
}
