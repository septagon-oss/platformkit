package internal_test

// Review round 3 of T-0115, at the module. `TestTwoLifecycleCommandsOnOneRowSettleOnce`
// pins one half of the rule the `FOR UPDATE` exists for: two of the *same* verb settle to one
// act, because the second re-reads the row as the first committed it and takes its idempotent
// branch. That leaves the other half untested, and it is the half that ends a customer: a
// verb racing a `Delete`.
//
// `Delete` is the only lifecycle write that removes the row from every reader's view, and the
// five verbs beside it decide what to do by reading the row first. If the re-read behind the
// lock did not apply the `deleted_at IS NULL` predicate — the one the comment on `lock` says
// makes four readers of a half-written column into four readers of a verb — the transaction
// that waited would act on a retired customer: a `suspend` would set `status` on a row nobody
// can see, and `add-host` would re-attach a hostname the delete had just released to the
// platform, which is the outage `Delete`'s own comment calls "a hostname owned by a customer
// nobody can see". Each would publish its verb event and its mirror in the trail of a tenant
// that is gone, so the operator's audit would record an act on a customer the act did not
// reach.
//
// The case therefore holds the delete open with its lock taken, asks two other verbs in that
// window, and asserts what the fixed code answers: `crud.ErrNotFound` — the same answer the
// same verb gives after the delete, which is what a request without the delete in front of it
// prints too — and no outbox row, no `status` column moved and no host row behind it. It reads
// no message anywhere, so it says nothing about the wording of a refusal.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/trace"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant/internal"
	"github.com/septagon-oss/platformkit/modules/user"
)

func TestALifecycleVerbThatRacesADeleteFindsNoTenantToActOn(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, auth.Migrations)
	svc := internal.NewService(nil, nil)
	ctx := trace.With(t.Context(), trace.New())

	var customer uuid.UUID
	if err := dbtest.System(ctx, conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		if _, err := internal.Bootstrap(ctx, tx, svc, contracts.NewTenant{
			Slug: "installation", Name: "This installation", Host: "ops.example.com",
		}); err != nil {
			return err
		}
		created, err := svc.Create(ctx, tx, contracts.NewTenant{Slug: "acme", Name: "Acme", Host: "acme.example.com"})
		if err != nil {
			return err
		}
		customer = created.ID
		// A second name, so `add-host` in the window below is a verb that has work to
		// do rather than one refused by the last-host floor for a reason of its own.
		_, err = svc.AddHost(ctx, tx, created.ID, "www.acme.example.com", false)
		if err != nil {
			return err
		}
		return tx.DB().Exec("DELETE FROM platformkit_outbox").Error
	}); err != nil {
		t.Fatalf("install an installation with one customer at two hosts: %v", err)
	}

	const hold = 400 * time.Millisecond
	retiring, started, release := make(chan error, 1), make(chan struct{}), make(chan struct{})
	go func() {
		retiring <- dbtest.System(context.Background(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
			if _, err := svc.Delete(ctx, tx, customer, contracts.Delete{Confirm: "acme"}); err != nil {
				return err
			}
			close(started) // the row lock is held from here until the transaction below ends
			<-release
			return nil
		})
	}()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("the delete never got as far as holding the row")
	}

	// The two verbs that raced it, each in its own transaction that commits whatever they
	// answer: a module that relied on its caller to roll back the refusal would be caught
	// by the outbox and column counts below, not by a rollback nobody promised.
	type answer struct {
		verb string
		err  error
	}
	verbs := []struct {
		name string
		call func(context.Context, db.Tx[db.System]) error
	}{
		{"suspend", func(ctx context.Context, tx db.Tx[db.System]) error {
			_, err := svc.Suspend(ctx, tx, customer)
			return err
		}},
		{"add-host", func(ctx context.Context, tx db.Tx[db.System]) error {
			_, err := svc.AddHost(ctx, tx, customer, "new.acme.example.com", false)
			return err
		}},
	}
	answers := make(chan answer, len(verbs))
	began := time.Now()
	for _, v := range verbs {
		go func() {
			var refused error
			committed := dbtest.System(context.Background(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
				refused = v.call(ctx, tx)
				return nil
			})
			if committed != nil {
				answers <- answer{verb: v.name, err: committed}
				return
			}
			answers <- answer{verb: v.name, err: refused}
		}()
	}
	time.Sleep(hold)
	close(release)

	for range verbs {
		select {
		case a := <-answers:
			if !errors.Is(a.err, crud.ErrNotFound) {
				t.Errorf("%s of a tenant being deleted in another transaction = %v, want ErrNotFound: the verb "+
					"waited behind the delete's row lock and then acted on a customer that is gone.", a.verb, a.err)
			}
		case <-time.After(20 * time.Second):
			t.Fatal("a verb racing the delete never returned; the row lock outlived its transaction")
		}
	}
	if waited := time.Since(began); waited < hold/2 {
		t.Fatalf("both verbs returned after %v, want them to have waited behind the delete", waited)
	}
	if err := <-retiring; err != nil {
		t.Fatalf("the delete: %v", err)
	}

	// What the two refused verbs left behind: nothing. One deleted event and one mirror, the
	// delete's own; no suspension, no host row, and no status column moved on the retired row.
	if err := dbtest.System(context.Background(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		var names []string
		if err := tx.DB().Table("platformkit_outbox").Pluck("name", &names).Error; err != nil {
			return err
		}
		for _, n := range names {
			if n != contracts.EventDeleted && n != contracts.EventLifecycleRecorded {
				t.Errorf("a verb that found no tenant to act on published %q; the trail holds %v", n, names)
			}
		}
		var hosts int64
		if err := tx.DB().Table("tenant_hosts").Where("tenant_id = ?", customer).
			Count(&hosts).Error; err != nil {
			return err
		}
		if hosts != 0 {
			t.Errorf("%d tenant_hosts rows are left for the retired tenant, want none: a verb that raced "+
				"the delete re-attached a name the delete had released.", hosts)
		}
		var status string
		var retired int64
		if err := tx.DB().Table("tenants").Select("status").Where("id = ?", customer).
			Scan(&status).Error; err != nil {
			return err
		}
		if err := tx.DB().Table("tenants").Where("id = ? AND deleted_at IS NOT NULL", customer).
			Count(&retired).Error; err != nil {
			return err
		}
		if retired != 1 || status != contracts.StatusActive {
			t.Errorf("the retired tenant reads status %q with %d retired rows, want %q and 1: the verbs that "+
				"refused wrote a column.", status, retired, contracts.StatusActive)
		}
		return nil
	}); err != nil {
		t.Fatalf("read what the two racing verbs left: %v", err)
	}

	// And the name the refused `add-host` tried to attach is free for the next customer —
	// the assertion that keeps the count above from being satisfied by a write that failed
	// for a reason nobody would want to ship.
	var taken int
	if err := admin.QueryRowContext(context.Background(),
		`SELECT count(*) FROM tenant_hosts WHERE host = $1`, "new.acme.example.com").Scan(&taken); err != nil {
		t.Fatalf("look up the name the refused add-host asked for: %v", err)
	}
	if taken != 0 {
		t.Errorf("new.acme.example.com is served to %d tenants after a refused add-host, want 0", taken)
	}
}
