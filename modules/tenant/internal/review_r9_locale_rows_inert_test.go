package internal_test

// A reviewer's case, T-0111 review round 9 (the last round, HIGH-only remit).
//
// `fdb5556` deleted `migrations/000028_tenant_locale.up.sql`'s backfill — the
// `INSERT INTO tenant_locales … SELECT id, default_locale FROM tenants` — and stated one
// consequence out loud: "Nothing about what a request is answered in moves", because the
// two reads of the table take the default out on the way read (`localesOf`'s
// `WHERE tenant_id = ? AND locale <> ?`, and `List`'s `without(…, t.DefaultLocale)`), so
// the column answers for a tenant that has no row beside it.
//
// That sentence is the whole reason the write could simply go, and no case in the
// repository ran it. A test database is migrated empty, so no test has ever held a tenant
// in the state the file now leaves behind — no `tenant_locales` row at all — which is the
// state of every tenant of every deployment that predates the migration. `grep -rn
// "DELETE FROM tenant_locales" modules/tenant/internal/*_test.go` found none before this file.
//
// So this file puts one tenant in each of the three shapes and reads it the way a request
// is answered: created today (a row for its default, written by `Create`), left by the
// migration (no row), and backfilled (the row the deleted statement wrote). The first is
// the control — the read that proves the reader works — and the two that follow are
// compared against it. What is asserted is that the language a tenant is served in is the
// same in all three. That is the claim, and the day the row stops being inert this case
// says so: a read that started treating the table as authoritative would leave the
// no-row tenant served in nothing, and a read that started counting the default's own row
// would put the default into the set it says it excludes.
//
// The last leg asks the thing a deployment actually does with a tenant the migration left
// with no rows: an operator declares its languages, and it lands.

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant/internal"
	"github.com/septagon-oss/platformkit/modules/user"
)

// served is what one read answers: the language a request that brings nothing is answered
// in, and the set an Accept-Language list is intersected with.
type served struct {
	defaults  string
	languages string // the set, joined by ",", so one comparison says which entry moved
	stored    []string
	served    string // what the loader hands the request pipeline: the kernel's own pair
}

func readInTx(ctx context.Context, tx db.Tx[db.System], svc *internal.Service,
	id uuid.UUID) (served, error) {
	out := served{}
	t, err := svc.Get(ctx, tx, id)
	if err != nil {
		return out, err
	}
	out.defaults, out.languages = t.DefaultLocale, strings.Join(t.Locales, ",")
	if err := tx.DB().Table("tenant_locales").Where("tenant_id = ?", id).
		Order("locale").Pluck("locale", &out.stored).Error; err != nil {
		return out, err
	}
	// The read every request makes, which is where a stored language becomes an answer:
	// `ByHost` is `httpx.TenantLoader`, and the kernel negotiates against what it returns.
	who, err := svc.ByHost(ctx, tx, "r9-inert.example.com")
	if err != nil {
		return out, err
	}
	if who.Languages != nil {
		out.served = who.Languages.Default + "|" + strings.Join(who.Languages.Others, ",")
	}
	return out, nil
}

func TestATenantLeftWithNoLanguageRowIsServedInTheLanguageItsColumnHolds(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, auth.Migrations)
	svc := internal.NewService(nil, []string{"en", "pt-PT"})
	ctx := t.Context()

	var id uuid.UUID
	if err := dbtest.System(ctx, conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		created, err := svc.Create(ctx, tx, contracts.NewTenant{
			Slug: "r9-inert", Name: "R9 Inert", Host: "r9-inert.example.com"})
		if err != nil {
			return err
		}
		id = created.ID
		return nil
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// The three shapes of one tenant, in the order a deployment meets them.
	shapes := []struct {
		name    string
		prepare func(tx db.Tx[db.System]) error
	}{
		{"as a create leaves it — a row for its default", nil},
		{"as migrations/000028 now leaves it — no row at all", func(tx db.Tx[db.System]) error {
			return tx.DB().Exec(`DELETE FROM tenant_locales WHERE tenant_id = ?`, id).Error
		}},
		{"as the deleted backfill left it — a row for the default", func(tx db.Tx[db.System]) error {
			if err := tx.DB().Exec(`DELETE FROM tenant_locales WHERE tenant_id = ?`, id).Error; err != nil {
				return err
			}
			return tx.DB().Exec(`INSERT INTO tenant_locales (tenant_id, locale) VALUES (?, 'en')`, id).Error
		}},
	}

	var first served
	for i, s := range shapes {
		var got served
		err := dbtest.System(ctx, conn, func(ctx context.Context, tx db.Tx[db.System]) error {
			if s.prepare != nil {
				if err := s.prepare(tx); err != nil {
					return err
				}
			}
			var err error
			got, err = readInTx(ctx, tx, svc, id)
			return err
		})
		if err != nil {
			t.Fatalf("read the tenant %s: %v", s.name, err)
		}
		if i == 0 {
			first = got
			// The control, and the only door to what follows: the reader works, and a
			// tenant created today is served in the one language its copy is written in.
			if got.defaults != "en" || got.languages != "" || got.served != "en|" || len(got.stored) != 1 {
				t.Fatalf("control: a tenant created today is served in %q with the set %q and the "+
					"rows %q, and the host resolution offers %q, not the one language its create declares",
					got.defaults, got.languages, got.stored, got.served)
			}
			continue
		}
		// The claim `fdb5556` makes about its own deletion, asked of the tables.
		if got.defaults != first.defaults || got.languages != first.languages || got.served != first.served {
			t.Errorf("a tenant %s is served in %q with the set %q, and a host resolution offers %q, "+
				"where the same tenant as a create is served in %q with the set %q and a host resolution "+
				"offers %q: what a request is answered in moved with the row, so the language lives in the "+
				"table and not in the column the migration says answers for a tenant that has no row beside it",
				s.name, got.defaults, got.languages, got.served,
				first.defaults, first.languages, first.served)
		}
	}

	// What a deployment does with a tenant the migration left with no rows: its people are
	// given a second language by the operator's command, starting from that state.
	if err := dbtest.System(ctx, conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		if err := tx.DB().Exec(`DELETE FROM tenant_locales WHERE tenant_id = ?`, id).Error; err != nil {
			return err
		}
		_, err := svc.SetLocale(ctx, tx, id, contracts.SetLocale{Default: "pt-PT", Supported: []string{"en"}})
		return err
	}); err != nil {
		t.Fatalf("declaring languages for a tenant the migration left with no rows: %v", err)
	}
	var after served
	if err := dbtest.System(ctx, conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		var err error
		after, err = readInTx(ctx, tx, svc, id)
		return err
	}); err != nil {
		t.Fatalf("read the declaration back: %v", err)
	}
	if after.defaults != "pt-PT" || !slices.Equal([]string{after.languages}, []string{"en"}) {
		t.Errorf("the declaration left %q with the set %q, not Portuguese with English beside it: a "+
			"tenant the migration gave no rows cannot be given a language", after.defaults, after.languages)
	}
	// The default is among the stored rows, which is the state `contracts.SetLocale` says
	// the command exists to refuse anything else than.
	if !slices.Contains(after.stored, after.defaults) {
		t.Errorf("the declaration stored %v without its default %q", after.stored, after.defaults)
	}
}
