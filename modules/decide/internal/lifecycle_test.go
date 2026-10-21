package internal_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	delivery "github.com/septagon-oss/platformkit/kit/events/transport"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/richtext"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/decide/contracts"
	"github.com/septagon-oss/platformkit/modules/decide/contracts/decidetest"
	"github.com/septagon-oss/platformkit/modules/decide/internal"
)

func TestSignedInvitesRevokeAndRecheckAdmission(t *testing.T) {
	w := newWorld(t)
	w.run(t, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		snap := decidetest.Seed()
		snap.Question.AllowLinkJoin = true
		return put(ctx, tx, snap)
	})
	var invite contracts.InviteReply
	w.run(t, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		invite, err = w.svc.IssueInvite(ctx, tx, contracts.IssueInviteInput{QuestionInput: contracts.QuestionInput{QuestionID: decidetest.Q, IfRevision: 1}, ExpiresAt: decidetest.At.Add(time.Hour)})
		return err
	})
	joining := tenancy.WithPrincipal(w.ctx, tenancy.Principal{UserID: decidetest.Evan})
	for _, token := range []string{invite.Token + "x", "invalid", ""} {
		err := db.Run(joining, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			out, err := w.svc.Join(ctx, tx, contracts.JoinInput{QuestionID: decidetest.Q, Token: token})
			if !errors.Is(err, contracts.ErrInvite) || out.Participant.ID != uuid.Nil {
				t.Fatalf("forged credential: %+v %v", out, err)
			}
			return err
		})
		if !errors.Is(err, contracts.ErrInvite) {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := db.Run(joining, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			out, err := w.svc.Join(ctx, tx, contracts.JoinInput{QuestionID: decidetest.Q, Token: invite.Token})
			if err == nil && (out.Participant.PersonID != decidetest.Evan || out.Question.Revision != 3) {
				t.Fatalf("join: %+v", out)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	w.run(t, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := w.svc.RevokeInvite(ctx, tx, contracts.QuestionInput{QuestionID: decidetest.Q, IfRevision: 3})
		return err
	})
	err := db.Run(joining, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		out, err := w.svc.Join(ctx, tx, contracts.JoinInput{QuestionID: decidetest.Q, Token: invite.Token})
		if !errors.Is(err, contracts.ErrInvite) || !reflect.DeepEqual(out, contracts.ParticipantChange{}) {
			t.Fatalf("revoked admission: %+v %v", out, err)
		}
		return err
	})
	if !errors.Is(err, contracts.ErrInvite) {
		t.Fatal(err)
	}
}

func TestVisibleQuestionsCountBeforePaginationAndExportNoSecrets(t *testing.T) {
	w := newWorld(t)
	w.run(t, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		snap := decidetest.Seed()
		snap.Question.InviteGeneration, snap.Question.InviteExpiresAt = 3, new(decidetest.At.Add(time.Hour))
		if err := put(ctx, tx, snap); err != nil {
			return err
		}
		page, err := w.svc.Questions(ctx, tx, contracts.QuestionQuery{Offset: 100, Limit: 1})
		if err != nil || page.Total != 1 || len(page.Items) != 0 {
			t.Fatalf("empty page: %+v %v", page, err)
		}
		stranger := tenancy.WithPrincipal(ctx, tenancy.Principal{UserID: decidetest.Evan})
		page, err = w.svc.Questions(stranger, tx, contracts.QuestionQuery{})
		if err != nil || page.Total != 0 || len(page.Items) != 0 {
			t.Fatalf("private count: %+v %v", page, err)
		}
		out, err := w.svc.Export(ctx, tx, decidetest.Q)
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(out)
		if err != nil {
			return err
		}
		var fields map[string]any
		if err := json.Unmarshal(encoded, &fields); err != nil {
			return err
		}
		question := fields["question"].(map[string]any)
		for _, name := range []string{"inviteGeneration", "inviteExpiresAt", "token", "tenantId"} {
			if _, ok := question[name]; ok {
				t.Fatalf("export leaked %s", name)
			}
		}
		denied, err := w.svc.Export(stranger, tx, decidetest.Q)
		if !errors.Is(err, contracts.ErrDenied) || !reflect.DeepEqual(denied, contracts.Export{}) {
			t.Fatalf("private export: %+v %v", denied, err)
		}
		return nil
	})
}

type tenantList struct{}

func (tenantList) List(context.Context, db.Tx[db.System]) ([]tenancy.Tenant, error) {
	return []tenancy.Tenant{{ID: decidetest.Tenant}}, nil
}

var _ jobs.TenantLister = tenantList{}

func TestDeadlineSweepClosesAtEqualityOnce(t *testing.T) {
	w := newWorld(t)
	w.run(t, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		snap := decidetest.Seed()
		snap.Question.ClosesAt = new(decidetest.At)
		return put(ctx, tx, snap)
	})
	job := w.svc.Deadlines(tenantList{}, time.Minute)
	for range 2 {
		if err := job.Run(t.Context(), w.conn); err != nil {
			t.Fatal(err)
		}
	}
	w.run(t, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		q, err := w.svc.Question(ctx, tx, decidetest.Q)
		if err != nil {
			return err
		}
		emitted, err := names(tx)
		if err != nil {
			return err
		}
		if q.Status != contracts.StatusClosed || q.Revision != 2 || q.ClosedAt == nil || !q.ClosedAt.Equal(decidetest.At) || !reflect.DeepEqual(emitted, []string{contracts.EventClosed}) {
			t.Fatalf("deadline: %+v %v", q, emitted)
		}
		return nil
	})
}

type brokenUses struct{ err error }

func (p brokenUses) SetUses(_ context.Context, tx db.Tx[db.Tenant], in rest.UsesInput) error {
	if err := tx.DB().Exec("INSERT INTO decide_test_uses (record) VALUES (?)", in.Record).Error; err != nil {
		return err
	}
	return p.err
}

func TestFileUseAndOutboxFailuresRollbackTheWholeCommand(t *testing.T) {
	for _, failure := range []string{"file ledger", "outbox"} {
		t.Run(failure, func(t *testing.T) {
			w := newWorld(t)
			if failure == "file ledger" {
				if _, err := w.admin.ExecContext(t.Context(), "CREATE TABLE decide_test_uses (record uuid)"); err != nil {
					t.Fatal(err)
				}
				var err error
				w.svc, err = internal.New(currentAccess{}, w.clock.Now, contracts.InviteKey("a-secret-for-isolated-tests-only-32"), brokenUses{errors.New("ledger unavailable")}, rest.ShowNothing{}, richtext.RejectImages{})
				if err != nil {
					t.Fatal(err)
				}
				err = db.Run(w.ctx, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
					out, err := w.svc.Open(ctx, tx, contracts.OpenInput{Title: "Atomic", Options: []contracts.OptionInput{{Title: "A"}}})
					if err == nil || out.ID != uuid.Nil {
						t.Fatalf("file failure returned row: %+v %v", out, err)
					}
					return err
				})
				if err == nil {
					t.Fatal("file failure committed")
				}
				var total int
				if err := w.admin.QueryRowContext(t.Context(), "SELECT (SELECT count(*) FROM decide_questions)+(SELECT count(*) FROM decide_test_uses)").Scan(&total); err != nil {
					t.Fatal(err)
				}
				if total != 0 {
					t.Fatal("partial file write")
				}
				return
			}
			w.run(t, func(ctx context.Context, tx db.Tx[db.Tenant]) error { return put(ctx, tx, decidetest.Seed()) })
			if _, err := w.admin.ExecContext(t.Context(), `CREATE FUNCTION refuse_event() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'outbox unavailable'; END $$;
				CREATE TRIGGER refuse_event BEFORE INSERT ON platformkit_outbox FOR EACH ROW EXECUTE FUNCTION refuse_event()`); err != nil {
				t.Fatal(err)
			}
			err := db.Run(w.ctx, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				out, err := w.svc.Vote(ctx, tx, contracts.VoteInput{QuestionID: decidetest.Q, OptionID: decidetest.A, Value: contracts.Yes})
				if err == nil || !reflect.DeepEqual(out, contracts.VoteReply{}) {
					t.Fatalf("outbox failure returned row: %+v %v", out, err)
				}
				return err
			})
			if err == nil {
				t.Fatal("outbox failure committed")
			}
			var total int
			if err := w.admin.QueryRowContext(t.Context(), "SELECT (SELECT count(*) FROM decide_votes)+(SELECT count(*) FROM platformkit_outbox)+(SELECT count(*) FROM decide_options WHERE first_matched_at IS NOT NULL)").Scan(&total); err != nil {
				t.Fatal(err)
			}
			if total != 0 {
				t.Fatal("partial ballot/outbox/latch")
			}
		})
	}
}

type notices struct{ fail bool }

func (n *notices) Matched(_ context.Context, tx db.Tx[db.Tenant], in contracts.Matched) error {
	for _, person := range in.Recipients {
		if err := tx.DB().Exec("INSERT INTO decide_test_notices VALUES (?)", person).Error; err != nil {
			return err
		}
		if n.fail {
			return errors.New("notification unavailable")
		}
	}
	return nil
}

type directTransport struct {
	events.Transport
	sink delivery.Sink
}

func (d *directTransport) Subscribe(_ context.Context, _, _ string, sink delivery.Sink) error {
	d.sink = sink
	return nil
}
func (d *directTransport) Publish(ctx context.Context, ev events.Event) error {
	return d.sink.Handle(ctx, ev)
}

func TestMatchDeliveryBatchAndClaimRetryTogether(t *testing.T) {
	w := newWorld(t)
	if _, err := w.admin.ExecContext(t.Context(), "CREATE TABLE decide_test_notices (person uuid)"); err != nil {
		t.Fatal(err)
	}
	transport := new(directTransport)
	n := &notices{fail: true}
	if err := events.Consume(t.Context(), w.conn, transport, []events.Subscription{internal.Tell(n)}); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(contracts.Matched{QuestionID: decidetest.Q, OptionID: decidetest.A, Recipients: []uuid.UUID{decidetest.Alice, decidetest.Bob}, Voters: []uuid.UUID{decidetest.Alice}, At: decidetest.At})
	if err != nil {
		t.Fatal(err)
	}
	ev := events.Event{ID: uuid.New(), TenantID: decidetest.Tenant, Name: contracts.EventMatched, Payload: payload, At: decidetest.At}
	if err := transport.Publish(t.Context(), ev); err == nil {
		t.Fatal("failed notice batch was accepted")
	}
	var total int
	if err := w.admin.QueryRowContext(t.Context(), "SELECT (SELECT count(*) FROM decide_test_notices)+(SELECT count(*) FROM platformkit_handled)").Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 0 {
		t.Fatal("failed batch or claim survived")
	}
	n.fail = false
	for range 2 {
		if err := transport.Publish(t.Context(), ev); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.admin.QueryRowContext(t.Context(), "SELECT count(*) FROM decide_test_notices").Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Fatalf("notices after redelivery: %d", total)
	}
}
