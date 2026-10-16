package internal_test

// A reviewer's case, T-0111 review round 3 — item 3 of the review checklist, which
// every round of this task has admitted it never executed:
//
//	round 1: (carried) / round 2: "two concurrent SetLocale calls remain
//	read-and-reasoned, not executed, by me or by anyone" / round 6: same.
//
// `SetLocale` takes no expected revision anywhere in `contracts.Service`, so there is
// no loser to refuse: two operators can say two different things about one tenant's
// languages at the same moment. The reason that is *probably* safe is the shape of the
// command — the `tenants` row is updated before `tenant_locales` is emptied and
// rewritten, so whichever transaction gets the row lock first makes the other wait
// until it has finished — and "probably" is not something a tenant's language should
// rest on while the failure mode is a tenant whose default is a language it is not
// served in, which is the exact state `contracts.SetLocale` says the module refuses
// rather than repairs.
//
// So this case runs the two commands, in two transactions, at one tenant, and asserts
// the property the contract states whatever the interleaving was: the default is one of
// the languages stored, and the stored set is one whole declaration rather than a
// splice of two. A transaction that loses to a row lock and comes back with a conflict
// is allowed — a refused write that writes nothing is the correct outcome — but a
// half-written set is not, and it is what an unexecuted case is worth.

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant/internal"
	"github.com/septagon-oss/platformkit/modules/user"
)

func TestTwoDeclarationsOfOneTenantArrivingAtOnceLeaveOneWholeSet(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, auth.Migrations)
	// The third argument is this control plane's own app, and the empty Name is the
	// deployment of one app — the one this case was written against, and the only one
	// it can be: a single composition owns the schema dbtest.Schema just migrated.
	svc := internal.NewService(nil, []string{"en", "pt-PT"}, "")
	installed(t, conn, svc)

	var id uuid.UUID
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		created, err := svc.Create(ctx, tx, contracts.NewTenant{
			Slug: "acme", Name: "Acme", Host: "acme.example.com"})
		if err != nil {
			return err
		}
		id = created.ID
		return nil
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Two declarations that cannot both be true at once, whose stored sets differ, so a
	// splice is visible: one tenant served in Portuguese with English beside it, one
	// served in English alone. A half-written result — the first default with the second
	// tenant's rows — leaves the default out of the set, which is the state this command
	// exists to refuse.
	rivals := []contracts.SetLocale{
		{Default: "pt-PT", Supported: []string{"en"}},
		{Default: "en", Supported: nil},
	}
	errs := make([]error, len(rivals))
	var wg sync.WaitGroup
	for i, in := range rivals {
		wg.Add(1)
		go func(i int, in contracts.SetLocale) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 30*time.Second)
			defer cancel()
			// `errors.Is(err, crud.ErrInvalid)` is not what is being looked for here, so
			// the error is kept rather than asserted: the point is what is in the tables
			// after both commands have returned.
			errs[i] = dbtest.System(ctx, conn, func(ctx context.Context, tx db.Tx[db.System]) error {
				_, err := svc.SetLocale(ctx, tx, id, in)
				return err
			})
		}(i, in)
	}
	wg.Wait()

	// A loser that came back with a conflict wrote nothing, which house rule 9 asks for;
	// both succeeding is also allowed. What is not allowed is a state no single command
	// wrote, so at least one of them has to have landed.
	if errs[0] != nil && errs[1] != nil {
		t.Fatalf("neither declaration landed: %v / %v — one command that arrived at a busy "+
			"tenant has to be able to say its languages, or a person cannot change their "+
			"tenant's language while another operator is doing the same", errs[0], errs[1])
	}
	for i, err := range errs {
		if err != nil {
			t.Logf("declaration %d (%q + %v) did not land: %v", i, rivals[i].Default, rivals[i].Supported, err)
		}
	}

	var (
		def    string
		stored []string
		who    tenancy.Tenant
	)
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		if err := tx.DB().Table("tenants").Where("id = ?", id).Pluck("default_locale", &def).Error; err != nil {
			return err
		}
		if err := tx.DB().Table("tenant_locales").Where("tenant_id = ?", id).
			Order("locale").Pluck("locale", &stored).Error; err != nil {
			return err
		}
		var err error
		who, err = svc.ByHost(ctx, tx, "acme.example.com")
		return err
	}); err != nil {
		t.Fatalf("read the tenant back: %v", err)
	}

	// The invariant `validLocales` builds every accepted request into: the default is a
	// row of its own, so a resolution never offers a language the set does not hold.
	if !slices.Contains(stored, def) {
		t.Errorf("the tenant's default %q is not among the languages stored for it %v: a request "+
			"that brings nothing it asked for is answered in a language this tenant does not serve, "+
			"and the page says so in its <html lang>", def, stored)
	}
	// One whole declaration or the other, never a splice. The two candidates are spelled
	// here because they are what the two commands above asked for, and nothing else can
	// be written by them.
	got := slices.Clone(stored)
	slices.Sort(got)
	if !(slices.Equal(got, []string{"en"}) || slices.Equal(got, []string{"en", "pt-PT"})) {
		t.Errorf("the two commands left %v, which neither of them asked for: a splice of two "+
			"declarations is a tenant served in a language nobody declared", got)
	}
	// And the read a page actually uses agrees with the table, default first, once.
	preferred := who.Languages.Preferred()
	if preferred[0] != def {
		t.Errorf("resolving the tenant offers %v, which does not lead with the tenant's own default %q",
			preferred, def)
	}
	if len(preferred) != len(slices.Compact(slices.Clone(preferred))) {
		t.Errorf("resolving the tenant offers %v, which names a language twice", preferred)
	}
}
