package user_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/user"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestTheProvidedLanguagePreferenceUsesTheUserRowsAndTenantBoundary(t *testing.T) {
	policy := pkit.NewModule("policy", func(w *pkit.Wiring) (module.Module, error) {
		pkit.Put[usercontracts.Administration](w, &usercontracts.AdministrationFunc{Ask: administering})
		pkit.Put[usercontracts.Granting](w, &usercontracts.GrantingFunc{Ask: func(context.Context, db.Tx[db.Tenant]) (bool, error) {
			return true, nil
		}})
		pkit.Put[usercontracts.LocalePolicy](w, &usercontracts.LocalePolicyFunc{Ask: speaks})
		return module.Module{Name: "policy"}, nil
	}, pkit.Provides[usercontracts.Administration](), pkit.Provides[usercontracts.Granting](), pkit.Provides[usercontracts.LocalePolicy]())
	var people usercontracts.Service
	var language usercontracts.LanguagePreference
	account := pkit.NewModule("account", func(w *pkit.Wiring) (module.Module, error) {
		people = pkit.Get[usercontracts.Service](w)
		language = pkit.Get[usercontracts.LanguagePreference](w)
		return module.Module{Name: "account"}, nil
	}, pkit.Needs[usercontracts.Service](), pkit.Needs[usercontracts.LanguagePreference]())
	if err := pkit.NewApp("language").Use(account, user.Module, policy).Validate(pkit.Deployment{Environment: pkit.Development}); err != nil {
		t.Fatal(err)
	}
	if people == nil || language == nil {
		t.Fatal("the lifecycle and language contracts must both be provided")
	}

	owner, conn := dbtest.Schema(t, user.Migrations)
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}
	ctx := tenancy.WithTenant(t.Context(), tenant)
	var person *usercontracts.User
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		person, err = people.Invite(ctx, tx, "ada@acme.test", "Ada")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	actor := tenancy.WithPrincipal(ctx, tenancy.Principal{UserID: person.ID})
	var initial usercontracts.Language
	if err := db.Run(actor, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		initial, err = language.OwnLanguage(ctx, tx)
		if err != nil {
			return err
		}
		_, err = language.SetPreferredLocaleAt(ctx, tx, initial.Revision, "pt-PT")
		return err
	}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		ctx  context.Context
		want error
	}{
		{"stale revision", actor, crud.ErrConflict},
		{"another tenant", tenancy.WithTenant(actor, tenancy.Tenant{ID: uuid.New(), Slug: "globex", Name: "Globex"}), crud.ErrNotFound},
		{"anonymous", ctx, usercontracts.ErrNoSignIn},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := db.Run(tc.ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				row, err := language.SetPreferredLocaleAt(ctx, tx, initial.Revision, "en")
				if !errors.Is(err, tc.want) || row != nil {
					t.Errorf("choice = %v, %v; want no row and %v", row, err, tc.want)
				}
				// Commit the caller's transaction so rollback cannot hide a refused write.
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
	if err := db.Run(actor, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := people.Get(ctx, tx, person.ID)
		if err != nil {
			return err
		}
		if row.PreferredLocale != "pt-PT" || row.LocaleRevision != initial.Revision+1 {
			t.Errorf("lifecycle read = %q at %d; want pt-PT at %d", row.PreferredLocale, row.LocaleRevision, initial.Revision+1)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var emitted int
	if err := owner.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_outbox WHERE name = $1`, usercontracts.EventLanguageSet).Scan(&emitted); err != nil {
		t.Fatal(err)
	}
	if emitted != 1 {
		t.Errorf("language events = %d; want only the accepted choice", emitted)
	}
}
