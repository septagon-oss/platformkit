package internal_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/idempotency"
	"github.com/septagon-oss/platformkit/kit/request"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/change"
	"github.com/septagon-oss/platformkit/modules/change/contracts"
	"github.com/septagon-oss/platformkit/modules/change/internal"
)

func TestProposalCompletesItsOriginOnlyAtApplyDeclineOrWithdrawal(t *testing.T) {
	for _, ending := range []string{"apply", "decline", "withdraw"} {
		t.Run(ending, func(t *testing.T) {
			admin, conn := dbtest.Schema(t, change.Migrations)
			subject := newCounter()
			svc := internal.NewService(bindingTo(subject))
			at := db.Now()
			ctx := tenancy.WithTenant(t.Context(), acme)
			reserve := func(person uuid.UUID) request.Operation {
				r := idempotency.Receipt{Ref: uuid.New(), ID: uuid.NewString(), Tenant: acme.ID, Caller: person, Person: person, Method: "POST", Route: "/proposal", Revision: 1, StartedAt: at}
				if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
					return idempotency.PostgresOperations().Reserve(ctx, tx, r)
				}); err != nil {
					t.Fatal(err)
				}
				return r.Operation()
			}
			origin := reserve(proposer)
			var row *contracts.Proposal
			if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				ctx = request.WithOperationClock(tenancy.WithActor(ctx, proposer), origin, func() time.Time { return at })
				row = propose(t, subject, svc, ctx, tx)
				if row.OperationRef == nil || row.OperationRef.String() != origin.Ref {
					t.Error("proposal lost origin")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			outcome := func() string {
				var value string
				if err := admin.QueryRow(`SELECT outcome FROM platformkit_operations WHERE id=$1`, origin.Ref).Scan(&value); err != nil {
					t.Fatal(err)
				}
				return value
			}
			if got := outcome(); got != "pending_approval" {
				t.Fatalf("proposed = %s", got)
			}
			who := reviewer
			if ending == "withdraw" {
				who = proposer
			}
			command := reserve(who)
			if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				ctx = request.WithOperation(tenancy.WithActor(ctx, who), command)
				var err error
				if ending == "withdraw" {
					row, err = svc.Withdraw(ctx, tx, row.ID, row.Revision)
				} else {
					verdict := contracts.VerdictApproved
					if ending == "decline" {
						verdict = contracts.VerdictDeclined
					}
					row, err = svc.Review(ctx, tx, row.ID, contracts.Review{ExpectedRevision: row.Revision, Verdict: verdict})
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if ending == "apply" {
				if got := outcome(); got != "pending_approval" {
					t.Fatalf("approved = %s", got)
				}
				command = reserve(reviewer)
				if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
					ctx = request.WithOperation(tenancy.WithActor(ctx, reviewer), command)
					var err error
					row, err = svc.Apply(ctx, tx, row.ID, row.Revision)
					return err
				}); err != nil {
					t.Fatal(err)
				}
			}
			want := "failed"
			if ending == "apply" {
				want = "succeeded"
			}
			if got := outcome(); got != want {
				t.Errorf("%s = %s", ending, got)
			}
			var actor, person string
			if err := admin.QueryRow(`SELECT actor::text,operation_person_id::text FROM platformkit_outbox WHERE name='platformkit.operation_outcome' AND operation_ref=$1 ORDER BY operation_revision DESC LIMIT 1`, origin.Ref).Scan(&actor, &person); err != nil {
				t.Fatal(err)
			}
			if actor != who.String() || person != proposer.String() {
				t.Errorf("actor=%s origin person=%s", actor, person)
			}
		})
	}
}
