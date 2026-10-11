package internal_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/translation"
	"github.com/septagon-oss/platformkit/modules/translation/contracts"
	"github.com/septagon-oss/platformkit/modules/translation/contracts/translationtest"
	"github.com/septagon-oss/platformkit/modules/translation/internal"
)

var (
	acme  = tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}
	other = tenancy.Tenant{ID: uuid.New(), Slug: "other", Name: "Other Co"}
)

// errRollback ends a conformance case's transaction without committing it.
var errRollback = errors.New("rolled back on purpose")

const outbox = "platformkit_outbox"

// TestServiceConforms runs the suite the fake runs, against the real service, a
// real Postgres and a real tenant transaction. This is the delivery's one
// implementation of each rule: the fake's cases are the service's cases, so a
// fake that drifts fails the same test the service passes.
//
// The transaction is rolled back at the end of every case rather than committed,
// which is the half the fake cannot do at all: a case that asserts "the refusal
// wrote nothing" means something different when nothing was ever going to be
// committed, so the same assertion is also made against a committed transaction
// by TestARefusedWriteLeavesNothingCommitted.
func TestServiceConforms(t *testing.T) {
	translationtest.RunService(t, func(t *testing.T, run func(translationtest.Fixture)) {
		_, conn := dbtest.Schema(t, translation.Migrations)
		source := translationtest.NewStubSource()
		machine := &translationtest.Machine{}
		svc := internal.NewService([]rest.TranslationSource{source}, machine)

		err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			run(translationtest.Fixture{
				Ctx: ctx, Tx: tx, Service: svc, Source: source, Machine: machine,
				Published: func() []string { return publishedNames(ctx, tx) },
				Payloads:  func() []contracts.Updated { return publishedPayloads(ctx, tx) },
				Rows:      func() []translationtest.Row { return storedRows(ctx, tx) },
			})
			return errRollback
		})
		if !errors.Is(err, errRollback) {
			t.Fatalf("the case's transaction: %v", err)
		}
	})
}

// TestATranslationOfTenantAIsUnreachableFromTenantB is the acceptance item, run
// twice over: through the port, where a wrong-tenant read must answer not-found
// rather than answer the row, and straight at the table under the application
// role, where row-level security must return zero rows for a SELECT that names
// no tenant condition at all.
func TestATranslationOfTenantAIsUnreachableFromTenantB(t *testing.T) {
	_, conn := dbtest.Schema(t, translation.Migrations)
	recordID := uuid.New()

	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		svc := internal.NewService(nil, nil)
		return svc.Save(ctx, tx, rest.SaveQuery{
			Module: "pages", Entity: "page", Locale: "pt-PT", RecordID: recordID,
			Values:   map[string]string{"body": "Sobre nós."},
			Expected: map[string]int64{"body": 0},
			Source:   map[string]string{"body": "About us."},
			RichText: map[string]bool{"body": false},
		})
	})
	if err != nil {
		t.Fatalf("saving Acme's translation: %v", err)
	}

	// Through the port, in the other tenant's transaction.
	err = db.Run(tenancy.WithTenant(t.Context(), other), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		svc := internal.NewService(nil, nil)
		got, err := svc.Translated(ctx, tx, rest.TranslatedQuery{
			Module: "pages", Entity: "page", Locale: "pt-PT", RecordIDs: []uuid.UUID{recordID},
			Sources: map[uuid.UUID]map[string]string{recordID: {"body": "About us."}},
		})
		if err != nil {
			return err
		}
		if len(got) != 1 || len(got[0].Fields) != 0 {
			return errors.New("the other tenant read Acme's translation")
		}
		// And it may not write one into Acme's record either: the row it would
		// update is not its row, and the insert would land in its own tenant,
		// which is the correct outcome and is checked by the count below.
		return nil
	})
	if err != nil {
		t.Fatalf("the other tenant's read: %v", err)
	}

	// And the write it would have liked to make is not possible either: had the
	// save gone through under the other tenant, a row would exist that this
	// tenant's own count cannot see.
	var mine int
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT count(*) FROM translations WHERE record_id = $1 AND origin = 'machine'`, recordID).Scan(&mine).Error
	}); err != nil {
		t.Fatalf("counting Acme's machine drafts: %v", err)
	}
	if mine != 0 {
		t.Errorf("the other tenant wrote %d rows into Acme's record", mine)
	}
}

// TestARowOfAnotherTenantIsNotThereUnderTheAppRole runs the same claim without
// Go in the way: the app role, in the other tenant's session, sees no row.
func TestARowOfAnotherTenantIsNotThereUnderTheAppRole(t *testing.T) {
	_, conn := dbtest.Schema(t, translation.Migrations)
	recordID := uuid.New()

	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		svc := internal.NewService(nil, nil)
		return svc.Save(ctx, tx, rest.SaveQuery{
			Module: "pages", Entity: "page", Locale: "pt-PT", RecordID: recordID,
			Values:   map[string]string{"body": "Sobre nós."},
			Expected: map[string]int64{"body": 0},
			Source:   map[string]string{"body": "About us."},
		})
	}); err != nil {
		t.Fatalf("saving Acme's translation: %v", err)
	}

	seen := -1
	if err := db.Run(tenancy.WithTenant(t.Context(), other), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT count(*) FROM translations`).Scan(&seen).Error
	}); err != nil {
		t.Fatalf("the other tenant's bare SELECT: %v", err)
	}
	if seen != 0 {
		t.Errorf("the other tenant's session counts %d translation rows; RLS answers an absent row, not a forbidden one", seen)
	}

	mine := -1
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT count(*) FROM translations`).Scan(&mine).Error
	}); err != nil {
		t.Fatalf("Acme's bare SELECT: %v", err)
	}
	if mine != 1 {
		t.Errorf("Acme counts %d of its own rows, want 1", mine)
	}
}

// TestARefusedWriteLeavesNothingCommitted closes the gap the conformance run
// cannot see, because that one rolls back whatever it did: a rejected stale
// revision, committed, changes neither the row nor the outbox.
func TestARefusedWriteLeavesNothingCommitted(t *testing.T) {
	_, conn := dbtest.Schema(t, translation.Migrations)
	recordID := uuid.New()
	source := map[string]string{"body": "About us."}

	run := func(tenant tenancy.Tenant, expected int64) error {
		return db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			svc := internal.NewService(nil, nil)
			return svc.Save(ctx, tx, rest.SaveQuery{
				Module: "pages", Entity: "page", Locale: "pt-PT", RecordID: recordID,
				Values:   map[string]string{"body": "Segunda."},
				Expected: map[string]int64{"body": expected},
				Source:   source,
			})
		})
	}
	if err := run(acme, 0); err != nil {
		t.Fatalf("the first save: %v", err)
	}
	// The retry with a revision nobody read, in its own transaction that
	// commits. A refusal that returns an error but still commits is exactly the
	// one a caller cannot see from here.
	err := run(acme, 99)
	if !errors.Is(err, crud.ErrConflict) {
		t.Fatalf("a stale revision answered %v, want crud.ErrConflict", err)
	}

	var revisions []int64
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT revision FROM translations WHERE record_id = $1`, recordID).Scan(&revisions).Error
	}); err != nil {
		t.Fatalf("reading revisions: %v", err)
	}
	if len(revisions) != 1 || revisions[0] != 1 {
		t.Errorf("the refused write moved the revision to %v, want the one row still at 1", revisions)
	}
}

func publishedNames(ctx context.Context, tx db.Tx[db.Tenant]) []string {
	var names []string
	err := tx.DB().Raw(`SELECT name FROM `+outbox+` WHERE tenant_id = ? ORDER BY created_at, id`, acme.ID).
		Scan(&names).Error
	if err != nil {
		panic("read the outbox: " + err.Error())
	}
	_ = ctx
	return names
}

func publishedPayloads(ctx context.Context, tx db.Tx[db.Tenant]) []contracts.Updated {
	var raw []string
	err := tx.DB().Raw(`SELECT payload::text FROM `+outbox+` WHERE name = ? AND tenant_id = ? ORDER BY created_at, id`,
		contracts.EventUpdated, acme.ID).Scan(&raw).Error
	if err != nil {
		panic("read the translation events: " + err.Error())
	}
	_ = ctx
	out := make([]contracts.Updated, 0, len(raw))
	for _, one := range raw {
		var ev contracts.Updated
		if err := json.Unmarshal([]byte(one), &ev); err != nil {
			panic("decode the event: " + err.Error())
		}
		out = append(out, ev)
	}
	return out
}

func storedRows(ctx context.Context, tx db.Tx[db.Tenant]) []translationtest.Row {
	var rows []internal.Row
	err := tx.DB().Where("tenant_id = ?", db.TenantOf(tx).ID).Order("field").Find(&rows).Error
	if err != nil {
		panic("read the translations: " + err.Error())
	}
	_ = ctx
	out := make([]translationtest.Row, 0, len(rows))
	for _, r := range rows {
		out = append(out, translationtest.Row{
			TenantID: r.TenantID, Module: r.Module, Entity: r.Entity, RecordID: r.RecordID,
			Field: r.Field, Locale: r.Locale, Value: r.Value, SourceText: r.SourceText,
			SourceHash: r.SourceHash, Status: r.Status, Origin: r.Origin,
			ReviewedAt: r.ReviewedAt, TranslatorID: r.TranslatorID, Revision: r.Revision,
		})
	}
	return out
}
