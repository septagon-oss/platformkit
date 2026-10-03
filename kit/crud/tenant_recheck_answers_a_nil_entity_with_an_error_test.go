package crud_test

// The fourth review's case against the one exported name this branch adds.
//
// Before 762a724 the tenant compare lived inside Update, behind Update's own
// `isNil` guard, and nothing could reach it without passing that guard. Round 5
// extracted it and exported it — `crud.RecheckTenant` — so that a caller which
// read a row under the lock and then decided to write nothing could ask the
// question without writing. The guard stayed behind, and the doc comment took
// over its job with a sentence: "As everywhere entity.BaseOf is reached
// directly, e is non-nil: Update's own guard says so first". That sentence is
// true of Update and of rest.updateRow and rest.deleteRow, and it is a
// precondition on an exported function of a published kit: the reason the
// function is exported at all is that a caller outside this package reaches for
// it, and such a caller has no Update above it. Every other door in this package
// answers a nil entity with ErrInvalid — measured, not assumed:
//
//	Update(nil)  = crud: invalid: there is nothing to update
//	Create(nil)  = crud: invalid: there is nothing to create
//	RecheckTenant(tx, nil)             PANICKED: invalid memory address…
//	RecheckTenant(tx, (*Task)(nil))    PANICKED: invalid memory address…
//
// So this case asserts the answer the exported door has to give — an error, in
// the shape the package's other doors use, never a panic that takes the request
// down with it. It passes the moment RecheckTenant grows the guard its two
// callers still carry; it fails while the precondition is a comment.

import (
	"context"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
)

func TestTheTenantRecheckAnswersANilEntityWithAnErrorAndNeverAPanic(t *testing.T) {
	conn := setup(t)
	own := &Task{Title: "ours"}
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		if err := crud.Create(ctx, tx, own); err != nil {
			t.Fatalf("Create: %v", err)
		}
	})

	// The two answers the door exists to give, asked first, so the nil cases
	// below cannot pass because the compare stopped working altogether.
	//
	// Read and written in one transaction; the foreign row is the same row with
	// another tenant's name on it, which is the shape a catalogue table hands the
	// kernel through GetForUpdate.
	foreign := *own
	foreign.TenantID = globex.ID
	typedNil := (*Task)(nil)
	as(t, conn, acme, func(_ context.Context, tx db.Tx[db.Tenant]) {
		if err := crud.RecheckTenant(tx, own); err != nil {
			t.Errorf("RecheckTenant on the transaction's own row = %v, want nil", err)
		}
		if err := crud.RecheckTenant(tx, &foreign); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("RecheckTenant on another tenant's row = %v, want ErrNotFound", err)
		}
	})

	// What Update and Create answer for nothing at all, for the record: the
	// comparison the case below holds the exported door to.
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		if err := crud.Update[*Task](ctx, tx, nil); !errors.Is(err, crud.ErrInvalid) {
			t.Fatalf("Update(nil) = %v, want ErrInvalid (the package's own answer, which the exported door has to match)", err)
		}
	})

	// And the exported door itself. The recover is the assertion's shape, not a
	// skip: a panic is what the case is about, and it must be reported against
	// the door rather than take the rest of the suite with it.
	for _, sent := range []struct {
		name string
		call func(tx db.Tx[db.Tenant]) error
	}{
		{"the untyped nil", func(tx db.Tx[db.Tenant]) error { return crud.RecheckTenant(tx, nil) }},
		{"a typed nil entity", func(tx db.Tx[db.Tenant]) error { return crud.RecheckTenant(tx, typedNil) }},
	} {
		var err error
		var panicked any
		func() {
			defer func() { panicked = recover() }()
			// attempt rather than as: the transaction that carried the refusal is
			// rolled back on purpose, which is what kit/crud's own helper is for.
			attempt(t, conn, acme, func(_ context.Context, tx db.Tx[db.Tenant]) {
				err = sent.call(tx)
			})
		}()
		if panicked != nil {
			t.Errorf("RecheckTenant(%s) panicked (%v); Update answers the same entity with %v, and an exported door may not crash the request that asks it",
				sent.name, panicked, crud.ErrInvalid)
			continue
		}
		if !errors.Is(err, crud.ErrInvalid) {
			t.Errorf("RecheckTenant(%s) = %v, want ErrInvalid, the answer kit/crud's other doors give for nothing to act on", sent.name, err)
		}
	}
}
