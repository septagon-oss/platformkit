package internal

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/webhook/contracts"
)

// The delivery defaults. Batch and the two timeouts are Deps fields with these
// values behind them; the ladder, the window and the cap are contracts'.
const (
	defaultBatch    = 20
	defaultTimeout  = 15 * time.Second
	defaultLease    = 30 * time.Second
	defaultInterval = 5 * time.Second

	// outcomeLock names the advisory transaction lock the outcome takes before
	// it reads the endpoint's fault clock. The critical section spans rows — the
	// delivery row this attempt belongs to and the endpoint row whose status and
	// failing_since it may change — and two concurrent outcomes for one endpoint
	// must not both decide it became disabled. GetForUpdate on the endpoint is
	// the aggregate root's lock; the advisory lock in front of it is what makes
	// "exactly one disablement per fault" a fact rather than a race two replicas
	// can both win.
	outcomeLock = "webhook-outcome"

	// userAgent names this program on the wire, so a customer's access log says
	// who called and not just that something did.
	userAgent = "platformkit-webhooks/1"

	// The three headers Standard Webhooks specifies, plus this program's own
	// ticket number.
	headerID          = "webhook-id"
	headerTimestamp   = "webhook-timestamp"
	headerSignature   = "webhook-signature"
	headerDelivery    = "x-platformkit-delivery"
	maxRedirects      = 3
	maxResponseLogged = 240
)

// Deliver is the module's clock: the queued rows go out. It is a job and not a
// subscription because the work is triggered by a deadline arriving, not by
// something having happened (docs/adr/0004), and because the retry ladder lives
// in rows this job owns rather than in the broker's five rungs.
//
// It runs per tenant, so the tenant transaction that claims a row is that
// tenant's own: row level security is what makes "never another tenant's row"
// true even of a mistyped query, and a wrong-tenant claim is not a bug this file
// can introduce.
//
// Three steps, and which transaction writes which row is the part a reviewer
// should check first:
//
//	claim    — one short tenant transaction: queued (or in_flight past its
//	           lease) rows become in_flight with FOR UPDATE SKIP LOCKED, the
//	           endpoint's secret is decrypted while that transaction is open,
//	           and it commits before any socket opens.
//	send     — no transaction at all. A lock is never held across somebody
//	           else's web server.
//	outcome  — a new tenant transaction: this row's terminal state, the retry
//	           row if the ladder has one, the endpoint's fault clock, and on
//	           the transition the disablement's status, its outbox event and
//	           its notice — one commit, so state and event cannot disagree.
//
// What the split buys and what it costs, stated: a crash between the send and
// the outcome transaction loses the record of an attempt that physically
// happened, and the lease expires and re-sends it. The module is at-least-once
// on the wire and says so; webhook-id is what a receiver deduplicates on, and
// attempt and seq make a duplicate visible in the log.
func Deliver(svc *Service, tenants jobs.TenantLister, every time.Duration, batch int, timeout, lease time.Duration) jobs.Job {
	if every <= 0 {
		every = defaultInterval
	}
	if batch <= 0 {
		batch = defaultBatch
	}
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	if lease <= 0 {
		lease = defaultLease
	}
	client := &http.Client{
		Transport: svc.Policy().transport(timeout),
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	return jobs.Job{
		Name:  "webhook-deliver",
		Every: every,
		Run: func(ctx context.Context, conn *db.Conn) error {
			return jobs.PerTenant(ctx, conn, tenants, func(ctx context.Context, conn *db.Conn, t tenancy.Tenant) error {
				claimed, err := svc.claim(ctx, conn, batch, lease)
				if err != nil || len(claimed) == 0 {
					return err
				}
				for _, attempt := range claimed {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					svc.send(ctx, conn, attempt, client)
				}
				return nil
			})
		},
	}
}

// attempt is one claimed row and the signing key it needs, both read in the
// claim transaction. The key travels in memory for the length of the send and
// nowhere else: it is never stored on the row, never logged, never in the
// outcome.
type attempt struct {
	row       contracts.Delivery
	endpoint  *contracts.Endpoint
	key       signingKey
	firstSent time.Time
}

// claim takes this tenant's due rows and commits before anything is dialled.
//
// A row already in flight whose lease expired is claimable too, which is the
// crash recovery: the attempt that physically happened and never got an outcome
// is re-sent rather than left in_flight until the retention job forgets it. That
// is the duplicate the README tells a receiver to deduplicate on webhook-id.
func (s *Service) claim(ctx context.Context, conn *db.Conn, batch int, lease time.Duration) ([]attempt, error) {
	var out []attempt
	err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		// The clock this transaction asks is the module's, the same one that wrote
		// available_at: the ladder's arithmetic and the drain's due-check have to
		// agree, and a row made due by one clock and judged by the other is a
		// retry that waits for a drift nobody intended. Row order and the log's own
		// timestamps stay the database's.
		now := s.now()
		var ids []uuid.UUID
		res := tx.DB().Raw("SELECT id FROM "+deliveryTable+
			" WHERE (state = 'queued' AND available_at <= ?)"+
			" OR (state = 'in_flight' AND attempted_at < ?)"+
			" ORDER BY available_at, seq LIMIT ? FOR UPDATE SKIP LOCKED",
			now, now.Add(-lease), batch).Scan(&ids)
		if res.Error != nil {
			return fmt.Errorf("webhook: claim the queued attempts: %w", res.Error)
		}
		if len(ids) == 0 {
			return nil
		}
		if err := tx.DB().Exec("UPDATE "+deliveryTable+
			" SET state = 'in_flight', attempted_at = ?, updated_at = ? WHERE id IN ?",
			now, now, ids).Error; err != nil {
			return fmt.Errorf("webhook: mark the claimed attempts in flight: %w", err)
		}
		var rows []contracts.Delivery
		if err := tx.DB().Table(deliveryTable).Where("id IN ?", ids).
			Order("available_at, seq").Find(&rows).Error; err != nil {
			return fmt.Errorf("webhook: read back the claimed attempts: %w", err)
		}
		for _, row := range rows {
			endpoint, err := crud.Get[*contracts.Endpoint](tx, row.EndpointID)
			if err != nil {
				// The endpoint is gone, or was soft-deleted, between the fan-out
				// and now. The row gets its terminal state with the reason; the
				// history stays.
				if dropErr := s.dropRow(tx, row.ID, contracts.StateDropped, 0, "the endpoint is gone"); dropErr != nil {
					return dropErr
				}
				continue
			}
			if !endpoint.IsEnabled() {
				// The disablement's own transaction ends every queued row, so a row
				// that reaches here for an endpoint that is disabled arrived between
				// that commit and this claim. It is ended the same way, with the same
				// reason in it, rather than dialling an address an administrator just
				// stopped trusting.
				if dropErr := s.dropRow(tx, row.ID, contracts.StateDropped, 0, contracts.ReasonEndpointDisabled); dropErr != nil {
					return dropErr
				}
				continue
			}
			key, err := s.CurrentSecret(tx, row.EndpointID)
			if err != nil {
				return err
			}
			out = append(out, attempt{row: row, endpoint: endpoint, key: key, firstSent: now})
		}
		return nil
	})
	return out, err
}

// send performs one POST and writes the outcome in its own transaction. It never
// returns an error to the job: one tenant's dead receiver must not stop the
// other tenants the lister returned, and one receiver's 500 must not stop the
// next row of the same tenant. What it cannot do is lose the row — every path
// ends in an outcome transaction, and the failure to write one is logged by the
// caller's own error.
func (s *Service) send(ctx context.Context, conn *db.Conn, a attempt, client *http.Client) {
	status, err := s.post(ctx, client, a)
	state, reason := classify(status, err)
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return s.record(ctx, tx, a, status, state, reason)
	}); err != nil && ctx.Err() == nil {
		// Logged where it happened rather than returned, because the row stays
		// in_flight and the lease will claim it again: the attempt is not lost,
		// and the other rows of this tenant are still this tick's work.
		s.logSendFailure(a, err)
	}
}

// post is the socket. The body is the stored payload's bytes and nothing else:
// the signature covers them, so a re-marshal per attempt would break a receiver
// that verified the first attempt and not the retry.
// The timestamp is the module's own clock, the one Deps.Now hands out: the ladder
// is computed with it, and the header a receiver compares against its own wall
// clock has to be the same time the sender scheduled with.
func (s *Service) post(ctx context.Context, client *http.Client, a attempt) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint.URL, bytes.NewReader(a.row.Payload))
	if err != nil {
		return 0, err
	}
	at := s.now()
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set(headerID, a.row.EventID.String())
	req.Header.Set(headerTimestamp, fmt.Sprintf("%d", at.Unix()))
	req.Header.Set(headerSignature, sign(a.key, a.row.EventID, at, a.row.Payload))
	req.Header.Set(headerDelivery, a.row.ID.String())

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	// The answer is read and discarded: what is kept is the status. A receiver's
	// HTML error page in this platform's database would be a copy of a
	// customer's content nobody asked for.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseLogged))
	return resp.StatusCode, nil
}

// classify turns an answer into one of the log's two terminal non-success
// states. dropped means "never again" and covers the address refusal, the closed
// window and the 4xx that will keep answering; failed means "and another try".
func classify(status int, err error) (state, reason string) {
	switch {
	case err != nil && AddressRefused(err):
		return contracts.StateDropped, contracts.ReasonAddressRefused + ": " + err.Error()
	case err != nil:
		return contracts.StateFailed, err.Error()
	case status >= 200 && status < 300:
		return contracts.StateSent, ""
	case retryable(status, nil):
		return contracts.StateFailed, retryReason(status)
	default:
		return contracts.StateDropped, retryReason(status)
	}
}

// record writes the outcome. One advisory lock, then the endpoint's row lock,
// then the decisions — in that order everywhere, so no two transactions can
// hold one each and wait.
func (s *Service) record(ctx context.Context, tx db.Tx[db.Tenant], a attempt, status int, state, reason string) error {
	if err := tx.DB().Exec("SELECT pg_advisory_xact_lock(hashtext(?)::bigint)", outcomeLock).Error; err != nil {
		return fmt.Errorf("webhook: take the outcome lock: %w", err)
	}
	endpoint, err := crud.GetForUpdate[*contracts.Endpoint](tx, a.row.EndpointID)
	if err != nil {
		return err
	}
	at := s.now()
	if err := s.dropRowColumns(tx, a.row.ID, state, status, reason, at); err != nil {
		return err
	}
	if state == contracts.StateSent {
		if err := tx.DB().Exec("UPDATE "+endpointTable+
			" SET failing_since = NULL, last_success_at = ? WHERE id = ?", at, endpoint.ID).Error; err != nil {
			return fmt.Errorf("webhook: clear the fault clock of %s: %w", endpoint.ID, err)
		}
		return nil
	}
	// The fault clock, stamped by the *first* failure after a success: COALESCE
	// is what makes it the first, so the three-day rule counts from the moment
	// the endpoint stopped working and not from the moment it was noticed.
	if err := tx.DB().Exec("UPDATE "+endpointTable+" SET failing_since = COALESCE(failing_since, ?) WHERE id = ?",
		at, endpoint.ID).Error; err != nil {
		return fmt.Errorf("webhook: stamp the fault clock of %s: %w", endpoint.ID, err)
	}
	if endpoint.FailingSince == nil {
		endpoint.FailingSince = &at
	}

	if state == contracts.StateFailed {
		// The endpoint's status is read from the row this transaction has just
		// locked, and it stops the ladder: an administrator's disable that landed
		// while this attempt was on the wire is the end of this event's attempts,
		// not one more rung. "Disable stops delivery now" is about the attempt
		// already in flight as much as about the queued ones the disablement drops.
		if availableAt, retry := nextAttempt(a.firstSent, a.row.Attempt, at); retry && endpoint.IsEnabled() {
			if err := s.queue(ctx, tx, a.row, contracts.CauseRetry, a.row.Attempt+1, availableAt); err != nil {
				return err
			}
		} else if err := s.note(tx, a.row.ID, ladderStop(endpoint)); err != nil {
			return err
		}
		// The fault rule, in this same transaction: the attempt that crossed the
		// three-day line is the one that disables the endpoint, and its event and
		// its notice commit with the state change that says so.
		_, err := s.DisableIfFailed(ctx, tx, endpoint.ID, at)
		return err
	}

	// A dropped attempt queues nothing. If it is the last thing that can happen
	// to this event, say so in the row rather than leaving the reader to work it
	// out from the absence of a successor.
	if state == contracts.StateDropped && reason == "" {
		return s.note(tx, a.row.ID, "the receiver answered "+retryReason(status))
	}
	return nil
}

// ladderStop is the sentence the last attempt of an event carries when no retry
// follows it, and the two reasons there is none: the window the brief gives closed
// around the failures, or somebody disabled the endpoint this attempt was asked
// for. A reader of the log is told which, because the two ask for different
// answers — one is "the receiver was dead for three days", the other is "you asked
// for this to stop".
func ladderStop(endpoint *contracts.Endpoint) string {
	if !endpoint.IsEnabled() {
		return contracts.ReasonEndpointDisabled
	}
	return contracts.ReasonRetryWindowClose
}

// queue appends one row: a retry of the same payload, or a replay's successor.
// The payload travels by reference to the row it belongs to, so the bytes are
// the ones the first attempt signed.
func (s *Service) queue(ctx context.Context, tx db.Tx[db.Tenant], row contracts.Delivery, cause string, attempt int, availableAt time.Time) error {
	_ = ctx
	res := tx.DB().Exec("INSERT INTO "+deliveryTable+
		" (tenant_id, endpoint_id, event_id, event_name, payload, attempt, cause, state, available_at)"+
		" SELECT tenant_id, endpoint_id, event_id, event_name, payload, ?, ?, 'queued', ? FROM "+deliveryTable+" WHERE id = ?",
		attempt, cause, availableAt, row.ID)
	if res.Error != nil {
		return fmt.Errorf("webhook: queue attempt %d of %s for %s: %w", attempt, row.EventID, row.EndpointID, res.Error)
	}
	return nil
}

// note adds a sentence to a row that already has its state.
func (s *Service) note(tx db.Tx[db.Tenant], id uuid.UUID, reason string) error {
	res := tx.DB().Exec("UPDATE "+deliveryTable+" SET reason = CASE WHEN reason = '' ? ELSE reason || '; ' || ? END WHERE id = ?",
		reason, reason, id)
	return res.Error
}

func (s *Service) dropRow(tx db.Tx[db.Tenant], id uuid.UUID, state string, status int, reason string) error {
	return s.dropRowColumns(tx, id, state, status, reason, s.now())
}

func (s *Service) dropRowColumns(tx db.Tx[db.Tenant], id uuid.UUID, state string, status int, reason string, at time.Time) error {
	res := tx.DB().Exec("UPDATE "+deliveryTable+
		" SET state = ?, status_code = ?, reason = ?, updated_at = ? WHERE id = ? AND state = 'in_flight'",
		state, status, reason, at, id)
	if res.Error != nil {
		return fmt.Errorf("webhook: record attempt %s: %w", id, res.Error)
	}
	return nil
}

// DisableIfFailed is the fault rule: an endpoint whose first failure is older
// than the window, with no success since, stops. It runs in the same transaction
// as the outcome that noticed it, publishes the event and calls the notice port
// there, so a disabled endpoint always has both its facts and always exactly one
// of each — and if the transaction rolls back, it has none.
//
// It is called by the delivery job's outcome pass rather than by a sweep, so the
// transition happens on the failure that crosses the line instead of up to a
// tick later, and the disablement of an endpoint is caused by the attempt that
// proved it dead.
func (s *Service) DisableIfFailed(ctx context.Context, tx db.Tx[db.Tenant], endpointID uuid.UUID, now time.Time) (bool, error) {
	endpoint, err := crud.GetForUpdate[*contracts.Endpoint](tx, endpointID)
	if err != nil {
		return false, err
	}
	if !endpoint.IsEnabled() || endpoint.FailingSince == nil {
		return false, nil
	}
	if now.Sub(*endpoint.FailingSince) < contracts.FaultWindow {
		return false, nil
	}
	if _, err := s.disable(ctx, tx, endpoint, contracts.ReasonNoDeliveryIn3d); err != nil {
		return false, err
	}
	if s.alerts == nil {
		// A deployment that tells people some other way still loses the endpoint,
		// still has the event and still has the log. The absence is a composition
		// choice, and this is where it is honoured.
		return true, nil
	}
	// The notice goes out beside the disablement, inside this transaction, behind a
	// savepoint — and the savepoint is the whole difference between a notifier that
	// is down and a notifier that does not exist. Without it the port's error would
	// be this transaction's error: the endpoint would stay enabled, the attempt would
	// stay in flight, the lease would re-claim it and a dead receiver would be
	// dialled again about every lease for as long as the notifier stayed down — a
	// stream of requests outside the ladder, which is the one thing the ladder exists
	// to make impossible. With it, the notice's own half is undone, the endpoint, its
	// outbox event and the log commit, and the failure is said out loud.
	//
	// A deployment that needs the notice to be durable does not read this port's
	// return value: it subscribes to webhook.endpoint.disabled, which commits with
	// the state change and is retried by the outbox.
	const notice = "webhook_notice"
	if err := tx.DB().Exec("SAVEPOINT " + notice).Error; err != nil {
		return false, fmt.Errorf("webhook: open the notice savepoint: %w", err)
	}
	if err := s.alerts.EndpointDisabled(ctx, tx, endpoint); err != nil {
		if back := tx.DB().Exec("ROLLBACK TO SAVEPOINT " + notice).Error; back != nil {
			// Nothing of this transaction survives, which is the right answer: an
			// outcome whose notice cannot even be forgotten is an outcome nobody can
			// write, and the lease will send the attempt again.
			return false, fmt.Errorf("webhook: tell %s its endpoint %s stopped: %w; and its half could not be rolled back: %v",
				endpoint.CreatedBy, endpoint.Name, err, back)
		}
		slog.ErrorContext(ctx, "webhook: could not tell an administrator their endpoint stopped",
			"endpoint", endpoint.ID, "to", endpoint.CreatedBy, "error", err)
		return true, nil
	}
	return true, nil
}

// logSendFailure is the one place the module admits it has no logging seam: the
// job's own error return carries what the tenant's transaction could not, and
// the row stays in_flight for the lease to re-claim.
func (s *Service) logSendFailure(a attempt, err error) {
	if s.failed == nil {
		return
	}
	s.failed(a.row.ID, err)
}

// SetFailureSink installs the function the delivery job reports an unwritable
// outcome to. It exists so the module's own test can see the one path that has
// no return value; nil means the path is silent, which is what it does in a
// deployment that reads its logs from the job's error instead.
func (s *Service) SetFailureSink(fn func(id uuid.UUID, err error)) { s.failed = fn }
