package db_test

import (
	"testing"
)

func TestASecondTenantsTransactionSeesNoRowTheFirstCommitted(t *testing.T) {
	admin, app, acme := acmeThings(t)
	other := newTenant("other")

	owner, p, _ := lazy(t, app, acme)
	t.Cleanup(func() { _ = p.Close(false) })
	insertThrough(t, owner, p, acme, "acme-row")
	if err := p.Close(true); err != nil {
		t.Fatalf("commit acme: %v", err)
	}
	if n := committed(t, admin); n != 1 {
		t.Fatalf("committed rows = %d, want 1", n)
	}

	ctx, q, _ := lazy(t, app, other)
	t.Cleanup(func() { _ = q.Close(false) })
	tx, err := q.Tx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := tx.DB().Raw("SELECT count(*) FROM things").Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("other tenant sees %d of acme's rows, want 0", n)
	}
}
