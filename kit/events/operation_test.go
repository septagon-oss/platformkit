package events_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/idempotency"
	"github.com/septagon-oss/platformkit/kit/request"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/task"
	taskcontracts "github.com/septagon-oss/platformkit/modules/task/contracts"
)

type operationSink struct {
	recorder
	sink events.Sink
}

func (s *operationSink) Subscribe(_ context.Context, _, _ string, sink events.Sink) error {
	s.sink = sink
	return nil
}

func TestAsyncCompletionAndTerminalEvidenceShareTheDeliveryTransaction(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		name := "completion"
		if terminal {
			name = "terminal failure"
		}
		t.Run(name, func(t *testing.T) {
			admin, conn := dbtest.Schema(t)
			at := db.Now()
			person := uuid.New()
			ctx := tenancy.WithTenant(t.Context(), acme)
			ctx, err := events.AdmitCommand(ctx, conn, idempotency.Scope{Tenant: acme.ID, Caller: person, Route: "work.start"}, person, func() time.Time { return at })
			if err != nil {
				t.Fatal(err)
			}
			op, _ := request.OperationFrom(ctx)
			if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				return events.Publish(ctx, tx, "work.accepted", struct{}{})
			}); err != nil {
				t.Fatal(err)
			}
			transport := &operationSink{}
			if err := events.Relay(t.Context(), conn, transport); err != nil {
				t.Fatal(err)
			}
			if len(transport.got) != 1 || transport.got[0].Operation != op {
				t.Fatalf("relay = %+v", transport.got)
			}
			ev := transport.got[0]
			retryErr := errors.New("provider not confirmed")
			subscription := events.Subscription{Module: "work", Name: "work.accepted", Handler: func(ctx context.Context, tx db.Tx[db.Tenant], ev events.Event) error {
				if _, ok := tenancy.PrincipalFrom(ctx); ok {
					t.Error("delivery restored a user session")
				}
				got, _ := request.OperationFrom(ctx)
				if got != op {
					t.Errorf("restored = %+v", got)
				}
				if err := events.Publish(ctx, tx, "work.intermediate", struct{}{}); err != nil {
					return err
				}
				return retryErr
			}}
			if terminal {
				subscription.Terminal = func(ctx context.Context, tx db.Tx[db.Tenant], ev events.Event, cause error) error {
					return events.RecordOutcome(ctx, tx, op.Revision, request.Failed, at.Add(time.Minute))
				}
			}
			if err := events.Consume(t.Context(), conn, transport, []events.Subscription{subscription}); err != nil {
				t.Fatal(err)
			}
			if err := transport.sink.Handle(t.Context(), ev); !errors.Is(err, retryErr) {
				t.Fatalf("retriable delivery = %v", err)
			}
			var intermediate int
			if err := admin.QueryRow(`SELECT count(*) FROM platformkit_outbox WHERE name='work.intermediate'`).Scan(&intermediate); err != nil {
				t.Fatal(err)
			}
			if intermediate != 0 {
				t.Error("refused delivery committed an intermediate event")
			}
			if terminal {
				if err := transport.sink.Dead(t.Context(), ev, retryErr); err != nil {
					t.Fatal(err)
				}
				if err := transport.sink.Dead(t.Context(), ev, retryErr); err != nil {
					t.Fatal(err)
				}
			} else {
				subscription.Handler = func(ctx context.Context, tx db.Tx[db.Tenant], ev events.Event) error {
					if err := events.RecordOutcome(ctx, tx, op.Revision, request.Succeeded, at.Add(time.Minute)); err != nil {
						return err
					}
					return events.Publish(ctx, tx, "work.completed", struct{}{})
				}
				if err := events.Consume(t.Context(), conn, transport, []events.Subscription{subscription}); err != nil {
					t.Fatal(err)
				}
				if err := transport.sink.Handle(t.Context(), ev); err != nil {
					t.Fatal(err)
				}
				if err := transport.sink.Handle(t.Context(), ev); err != nil {
					t.Fatal(err)
				}
			}
			var outcome string
			var revision, evidence, claims int
			if err := admin.QueryRow(`SELECT outcome,revision FROM platformkit_operations WHERE id=$1`, op.Ref).Scan(&outcome, &revision); err != nil {
				t.Fatal(err)
			}
			if err := admin.QueryRow(`SELECT count(*) FROM platformkit_outbox WHERE name='platformkit.operation_outcome' AND operation_ref=$1 AND operation_outcome=$2 AND operation_revision=2`, op.Ref, outcome).Scan(&evidence); err != nil {
				t.Fatal(err)
			}
			if err := admin.QueryRow(`SELECT count(*) FROM platformkit_handled WHERE event_id=$1`, ev.ID).Scan(&claims); err != nil {
				t.Fatal(err)
			}
			want := string(request.Succeeded)
			if terminal {
				want = string(request.Failed)
			}
			if outcome != want || revision != 2 || evidence != 1 || claims != 1 {
				t.Errorf("outcome=%s revision=%d evidence=%d claims=%d", outcome, revision, evidence, claims)
			}
		})
	}
}

func TestHistoricalDeliveryMasksInheritedOperation(t *testing.T) {
	_, conn := dbtest.Schema(t)
	transport := &operationSink{}
	ctx := request.WithOperation(t.Context(), request.Operation{ID: "unrelated", Ref: uuid.NewString(), Tenant: acme.ID.String(), Revision: 1})
	if err := events.Consume(ctx, conn, transport, []events.Subscription{{Module: "trail", Name: "work.old", Handler: func(ctx context.Context, tx db.Tx[db.Tenant], ev events.Event) error {
		if _, ok := request.OperationFrom(ctx); ok {
			t.Error("historical event inherited another intent")
		}
		return nil
	}}}); err != nil {
		t.Fatal(err)
	}
	if err := transport.sink.Handle(ctx, events.Event{ID: uuid.New(), Name: "work.old", TenantID: acme.ID, At: db.Now()}); err != nil {
		t.Fatal(err)
	}
}

func TestIntermediateDomainWriteDoesNotCompleteTheAsyncOrigin(t *testing.T) {
	admin, conn := dbtest.Schema(t, task.Migrations)
	ctx := tenancy.WithTenant(t.Context(), acme)
	at := db.Now()
	ctx, err := events.AdmitCommand(ctx, conn, idempotency.Scope{Tenant: acme.ID, Route: "work.fanout"}, uuid.Nil, func() time.Time { return at })
	if err != nil {
		t.Fatal(err)
	}
	op, _ := request.OperationFrom(ctx)
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return events.Publish(ctx, tx, "work.accepted", struct{}{})
	}); err != nil {
		t.Fatal(err)
	}
	transport := &operationSink{}
	if err := events.Relay(t.Context(), conn, transport); err != nil {
		t.Fatal(err)
	}
	if err := events.Consume(t.Context(), conn, transport, []events.Subscription{{Module: "work", Name: "work.accepted", Handler: func(ctx context.Context, tx db.Tx[db.Tenant], ev events.Event) error {
		_, err := task.Spec.CreateRow(ctx, tx, &taskcontracts.Task{Title: "intermediate task"})
		return err
	}}}); err != nil {
		t.Fatal(err)
	}
	if err := transport.sink.Handle(t.Context(), transport.got[0]); err != nil {
		t.Fatal(err)
	}
	var unknown bool
	var domain, evidence int
	if err := admin.QueryRow(`SELECT outcome IS NULL FROM platformkit_operations WHERE id=$1`, op.Ref).Scan(&unknown); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(`SELECT count(*) FILTER (WHERE name='task.task.created' AND operation_ref=$1 AND operation_outcome IS NULL),count(*) FILTER (WHERE name='platformkit.operation_outcome') FROM platformkit_outbox`, op.Ref).Scan(&domain, &evidence); err != nil {
		t.Fatal(err)
	}
	if !unknown || domain != 1 || evidence != 0 {
		t.Errorf("unknown=%v intermediate=%d evidence=%d", unknown, domain, evidence)
	}
}
