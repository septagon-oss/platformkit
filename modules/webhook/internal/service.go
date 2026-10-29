// Package internal is every implementation of the webhook module. Nothing
// outside modules/webhook can import it, which is the compiler enforcing the
// module boundary: a consumer takes contracts.Service.
//
// The module has two halves that never meet. The write half (this file) holds
// the secret, moves the lifecycle and queues work; the send half (deliver.go)
// reads a queued row, opens a socket and writes an outcome. Nothing here touches
// a network, and nothing on the send path takes a caller's word for what to send.
package internal

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/webhook/contracts"
)

// The three tables, named once each, in the files that own their DDL.
const (
	endpointTable = "webhook_endpoints"
	secretTable   = "webhook_endpoint_secrets"
	deliveryTable = "webhook_deliveries"
)

// Deps is what this module cannot make for itself. SecretKey and Catalogue are
// required — the first because every signature needs it and the second because
// every write is checked against it — and the module refuses to be constructed
// without them, at the composition site, rather than failing the first create.
type Deps struct {
	// SecretKey is the 32-byte key the signing secrets are wrapped with. It is
	// deployment configuration: a tenant can neither read it nor set it.
	SecretKey []byte

	// Catalogue is what this composition can emit, which is what an endpoint's
	// event list is checked against.
	Catalogue contracts.Catalogue

	// Alerts may be nil: a deployment that tells people some other way still
	// disables, still publishes, still writes the log. See contracts.Alerts.
	Alerts contracts.Alerts

	// Policy is the SSRF gate, carrying the deployment's AllowLocal flag.
	Policy AddressPolicy

	// Now is the clock the *sender* computes with: the backoff arithmetic and
	// the webhook-timestamp header. Row timestamps are the database's, because
	// two replicas must not disagree about the log's order; this one is a
	// function because it is the clock the receiver checks against itself, and
	// a test that steps it can falsify a three-day rule in a millisecond.
	Now func() time.Time
}

// Service is the write half: commands that hold the secret, move the lifecycle,
// or queue work. Every command locks the endpoint it is about with
// crud.GetForUpdate before reading the state it decides on, so two commands on
// one row settle in the order the database took the locks and a stale read
// cannot decide anything.
type Service struct {
	wrap      *box
	policy    AddressPolicy
	catalogue contracts.Catalogue
	alerts    contracts.Alerts
	now       func() time.Time

	// failed reports an outcome this module could not write. See SetFailureSink.
	failed func(id uuid.UUID, err error)
}

var _ contracts.Service = (*Service)(nil)

// NewService builds the service. A wrong key width, a missing catalogue and an
// empty event list are wiring mistakes, so they panic where they are written;
// NewServiceChecked answers the same refusals as an error.
func NewService(deps Deps) *Service {
	svc, err := NewServiceChecked(deps)
	if err != nil {
		panic(err.Error())
	}
	return svc
}

// NewServiceChecked builds the service and answers as an error the wiring every
// write depends on: the secret key is the 32 bytes one read hands it, the event
// list comes from the catalogue rather than from this module, and a key the
// envelope cannot be built on means this process must not start.
func NewServiceChecked(deps Deps) (*Service, error) {
	if len(deps.SecretKey) != 32 {
		return nil, fmt.Errorf("webhook: the secret key is %d bytes; it is 32, read once by the composition", len(deps.SecretKey))
	}
	if deps.Catalogue == nil {
		return nil, errors.New("webhook: a Catalogue is required: every write and the picker read it, and nothing can invent the list")
	}
	wrap, err := newBox(deps.SecretKey)
	if err != nil {
		return nil, err
	}
	now := deps.Now
	if now == nil {
		now = db.Now
	}
	return &Service{wrap: wrap, policy: deps.Policy, catalogue: deps.Catalogue, alerts: deps.Alerts, now: now}, nil
}

// Now is the module's clock, shared by every half: the sender stamps its header
// with it and the service schedules with it.
func (s *Service) Now() time.Time { return s.now() }

// Policy is the address gate, which the sender needs to build its client and the
// write path needs to refuse an address with.
func (s *Service) Policy() AddressPolicy { return s.policy }

// AlertsPort is the disablement notice, nil when the deployment wants none.
func (s *Service) AlertsPort() contracts.Alerts { return s.alerts }

// Create registers an endpoint and mints its secret.
//
// It is a hand-mounted route and not kit/rest's generated create for one reason:
// the answer is not the entity. The create is the only place, besides the
// rotate, where the signing secret exists in cleartext, and a Spec's create
// answers with the row — so either the secret is a field of the row, which puts
// it on a screen and in an event payload, or the create is this.
func (s *Service) Create(ctx context.Context, tx db.Tx[db.Tenant], e *contracts.Endpoint) (*contracts.Issued, error) {
	if e.Status == "" {
		e.Status = contracts.StatusEnabled
	}
	if err := e.Validate(ctx); err != nil {
		return nil, fmt.Errorf("%w: %v", crud.ErrInvalid, err)
	}
	if err := s.checkEvents(e.Events); err != nil {
		return nil, err
	}
	if reason := s.policy.Refuse(e.URL); reason != "" {
		return nil, fmt.Errorf("%w: %s: %s", crud.ErrInvalid, e.URL, reason)
	}
	// The actor is who a disablement notice addresses, so an endpoint with
	// nobody behind it is refused rather than left unable to tell anybody. A
	// create through a key with no user is a deployment's own act, and it names
	// itself the same way.
	actor, ok := tenancy.ActorFrom(ctx)
	if !ok || actor == uuid.Nil {
		return nil, fmt.Errorf("%w: an endpoint is registered by somebody, who a disablement notice can tell", crud.ErrInvalid)
	}
	e.CreatedBy = actor
	e.Status = contracts.StatusEnabled
	e.ID = uuid.New()
	if err := crud.Create(ctx, tx, e); err != nil {
		return nil, err
	}
	key, spelling, err := newKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	nonce, ciphertext, err := s.wrap.seal(key)
	if err != nil {
		return nil, err
	}
	res := tx.DB().Exec("INSERT INTO "+secretTable+
		" (endpoint_id, tenant_id, current_nonce, current_ciphertext, rotated_at)"+
		" VALUES (?, ?, ?, ?, ?)",
		e.ID, db.TenantOf(tx).ID, nonce, ciphertext, s.now())
	if res.Error != nil {
		return nil, fmt.Errorf("webhook: store the signing secret of %s: %w", e.ID, res.Error)
	}
	// The row, its secret and the event that says an endpoint exists are one
	// commit: a create that published without storing, or stored without
	// publishing, would be either a subscription nobody registered or a
	// registration nobody heard about. The payload is the entity, which carries
	// no secret — see the note on the type.
	if err := events.Publish(ctx, tx, contracts.EventEndpointCreated, e); err != nil {
		return nil, err
	}
	return &contracts.Issued{Endpoint: e, Secret: spelling, RotatedAt: e.CreatedAt}, nil
}

// Target changes what an endpoint is delivered and where. Both fields re-check
// what the create checked — the address policy with the deployment's flag, and
// the catalogue — which is why they are a command and not PATCH fields: a route
// that re-checks nothing would be a way to move every future delivery of an
// endpoint without anybody asking whether the new address is one this deployment
// dials.
func (s *Service) Target(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, url string, list contracts.EventNames) (*contracts.Endpoint, error) {
	endpoint, err := crud.GetForUpdate[*contracts.Endpoint](tx, id)
	if err != nil {
		return nil, err
	}
	before := *endpoint
	normalized, err := contracts.NormalizeURL(url)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", crud.ErrInvalid, err)
	}
	after := before
	after.URL, after.Events = normalized, list
	if err := after.Validate(ctx); err != nil {
		return nil, fmt.Errorf("%w: %v", crud.ErrInvalid, err)
	}
	if err := s.checkEvents(after.Events); err != nil {
		return nil, err
	}
	if reason := s.policy.Refuse(after.URL); reason != "" {
		return nil, fmt.Errorf("%w: %s: %s", crud.ErrInvalid, after.URL, reason)
	}
	if before.URL == after.URL && slices.Equal([]string(before.Events), []string(after.Events)) {
		// The same address and the same list is the same endpoint. Nothing to
		// write, and so nothing to publish: a retried click must not appear in
		// the trail twice.
		return endpoint, nil
	}
	endpoint.URL, endpoint.Events = after.URL, after.Events
	if err := crud.Update(ctx, tx, endpoint, "url", "events", "updated_at"); err != nil {
		return nil, err
	}
	// What the save moved, in the event that says the save happened. The kernel's
	// CRUD door diffs the row it wrote for an entity that carries a changes member;
	// this is a command and not a PATCH, so the diff is made here over the two
	// fields this command owns, after the UPDATE and before the publish, and
	// cleared on the way out because the response is the row and the diff belongs
	// to the event. A diff that cannot be computed fails the command: the
	// transaction rolls back with the UPDATE in it, and an endpoint whose
	// destination moved without a trail row saying what moved is the half-fact the
	// trail exists to prevent.
	changes, err := events.ChangesOf(&before, endpoint, "url", "events")
	if err != nil {
		return nil, fmt.Errorf("webhook: describe the destination change of %s: %w", id, err)
	}
	endpoint.SetChanges(changes)
	if err := events.Publish(ctx, tx, contracts.EventEndpointUpdated, endpoint); err != nil {
		return nil, err
	}
	endpoint.SetChanges(nil)
	return endpoint, nil
}

// Enable takes a disabled endpoint back and starts the fault clock again. It
// clears failing_since rather than leaving it, because a re-enabled endpoint that
// kept its old failure date would be disabled again on its first failed attempt.
//
// It publishes webhook.endpoint.enabled: an address that will be dialled again is
// a fact about the world, and the person a disablement told is the person an
// enablement is for. Enabling an endpoint that was already enabled changes nothing
// and publishes nothing.
func (s *Service) Enable(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.Endpoint, error) {
	endpoint, err := crud.GetForUpdate[*contracts.Endpoint](tx, id)
	if err != nil {
		return nil, err
	}
	if endpoint.Status == contracts.StatusEnabled {
		return endpoint, nil
	}
	endpoint.Status = contracts.StatusEnabled
	endpoint.DisabledAt, endpoint.DisabledReason = nil, ""
	endpoint.FailingSince = nil
	if err := crud.Update(ctx, tx, endpoint, "status", "disabled_at", "disabled_reason", "failing_since", "updated_at"); err != nil {
		return nil, err
	}
	return endpoint, events.Publish(ctx, tx, contracts.EventEndpointEnabled, endpoint)
}

// Disable stops delivery now. It is the administrator's version of the fault
// rule, and it shares the rule's two promises: the endpoint's history survives,
// and the queued attempts do not silently stall — each gets a terminal row
// naming the reason.
func (s *Service) Disable(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.Endpoint, error) {
	endpoint, err := crud.GetForUpdate[*contracts.Endpoint](tx, id)
	if err != nil {
		return nil, err
	}
	if endpoint.Status == contracts.StatusDisabled {
		return endpoint, nil
	}
	return s.disable(ctx, tx, endpoint, contracts.ReasonDisabledByAdmin)
}

// disable is the one write of the disabled state, so the fault rule and the
// administrator cannot drift: both stamp the moment, the reason, and drop the
// queued rows in the same transaction they were asked in.
func (s *Service) disable(ctx context.Context, tx db.Tx[db.Tenant], endpoint *contracts.Endpoint, reason string) (*contracts.Endpoint, error) {
	at := s.now()
	endpoint.Status = contracts.StatusDisabled
	endpoint.DisabledAt = &at
	endpoint.DisabledReason = reason
	if err := crud.Update(ctx, tx, endpoint, "status", "disabled_at", "disabled_reason", "updated_at"); err != nil {
		return nil, err
	}
	if err := s.dropQueued(tx, endpoint.ID, reason); err != nil {
		return nil, err
	}
	return endpoint, events.Publish(ctx, tx, contracts.EventEndpointDisabled, contracts.EndpointDisabled{
		EndpointID: endpoint.ID, Name: endpoint.Name, URL: endpoint.URL,
		Reason: reason, FailingSince: deref(endpoint.FailingSince, at), At: at,
	})
}

// DropQueued ends this endpoint's pending attempts with a reason. It is exported
// to this package only through the two callers that need it: an explicit
// disablement, and the delete hook.
func (s *Service) DropQueued(tx db.Tx[db.Tenant], id uuid.UUID, reason string) error {
	return s.dropQueued(tx, id, reason)
}

// dropQueued is the statement: every queued row becomes a dropped one, which
// keeps its payload and its place in the log. Nothing is deleted — a delete must
// not take the trail away.
func (s *Service) dropQueued(tx db.Tx[db.Tenant], id uuid.UUID, reason string) error {
	res := tx.DB().Exec("UPDATE "+deliveryTable+" SET state = 'dropped', reason = ?, updated_at = clock_timestamp()"+
		" WHERE endpoint_id = ? AND state = 'queued'", reason, id)
	if res.Error != nil {
		return fmt.Errorf("webhook: drop the queued attempts of %s: %w", id, res.Error)
	}
	return nil
}

// Rotate mints a new secret and moves the current one into the grace window.
//
// The window exists because a message already queued, or in a retry, was
// addressed to a receiver holding the old secret. The module signs with the
// current secret only — a signature is one key, and a receiver that gets two
// would have to guess — and the previous ciphertext stays for
// contracts.GraceWindow so the rotation record can tell a reader when the old
// key stopped working. Rotating twice inside one window drops the oldest.
func (s *Service) Rotate(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.Issued, error) {
	endpoint, err := crud.GetForUpdate[*contracts.Endpoint](tx, id)
	if err != nil {
		return nil, err
	}
	key, spelling, err := newKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	nonce, ciphertext, err := s.wrap.seal(key)
	if err != nil {
		return nil, err
	}
	at := s.now()
	// The old current becomes previous, with its expiry, in one statement: a
	// rotate that wrote two rows in two statements would leave an endpoint with
	// no usable key if it stopped in between.
	res := tx.DB().Exec("UPDATE "+secretTable+" SET"+
		" previous_nonce = current_nonce, previous_ciphertext = current_ciphertext,"+
		" previous_expires_at = ?, current_nonce = ?, current_ciphertext = ?, rotated_at = ?"+
		" WHERE endpoint_id = ?",
		at.Add(contracts.GraceWindow), nonce, ciphertext, at, id)
	if res.Error != nil {
		return nil, fmt.Errorf("webhook: rotate the signing secret of %s: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		// No secret row for an endpoint the caller can see is a defect in this
		// module's create, not a caller's mistake, and it is named as one.
		return nil, fmt.Errorf("webhook: endpoint %s has no signing secret to rotate", id)
	}
	return &contracts.Issued{Endpoint: endpoint, Secret: spelling, RotatedAt: at}, nil
}

// CurrentSecret decrypts the endpoint's signing key. It is the only reader of
// the ciphertext, and it is called by the sender alone: no route, screen, event
// or log line reaches it.
func (s *Service) CurrentSecret(tx db.Tx[db.Tenant], endpointID uuid.UUID) (signingKey, error) {
	var nonce, ciphertext []byte
	res := tx.DB().Raw("SELECT current_nonce, current_ciphertext FROM "+secretTable+" WHERE endpoint_id = ?", endpointID).
		Row().Scan(&nonce, &ciphertext)
	if res != nil {
		return nil, fmt.Errorf("webhook: read the signing secret of %s: %w", endpointID, res)
	}
	return s.decodeSecret(nonce, ciphertext)
}

// SecretWithinGrace decrypts the secret that was current until a rotation, while
// its window is open. Nothing signs with it; the module's own rotation record and
// a support question are what it answers, which is why it returns the spelling a
// human can compare rather than a key.
func (s *Service) SecretWithinGrace(tx db.Tx[db.Tenant], endpointID uuid.UUID, now time.Time) (string, time.Time, bool) {
	var nonce, ciphertext []byte
	var expires time.Time
	row := tx.DB().Raw("SELECT previous_nonce, previous_ciphertext, previous_expires_at FROM "+secretTable+
		" WHERE endpoint_id = ? AND previous_expires_at > ?", endpointID, now).Row()
	if err := row.Scan(&nonce, &ciphertext, &expires); err != nil {
		return "", time.Time{}, false
	}
	key, err := s.decodeSecret(nonce, ciphertext)
	if err != nil {
		return "", time.Time{}, false
	}
	return secretPrefix + base64.StdEncoding.EncodeToString(key), expires, true
}

func (s *Service) decodeSecret(nonce, ciphertext []byte) (signingKey, error) {
	plain, err := s.wrap.open(nonce, ciphertext)
	if err != nil {
		return nil, err
	}
	if len(plain) != secretBytes {
		return nil, fmt.Errorf("webhook: the stored secret is %d bytes, not %d", len(plain), secretBytes)
	}
	return plain, nil
}

// Replay is a person asking for one event to be sent again.
//
// It rechecks the three things a mutation rechecks: the actor's grant, which is
// the route's guard and is webhook:update because a replay sends; the tenant
// scope, which is row level security deciding — a delivery in another tenant is
// ErrNotFound here exactly as it is nowhere; and its own idempotence, which is
// what stands in for an expected revision: the command's body carries no revision
// because it changes no state, and the refusal that matters is "this pair already
// has an attempt waiting", which writes nothing, emits nothing and answers with
// the row that is already waiting.
//
// It queues; it does not send. The row it writes is cause='replay', and the log
// is the trail of the act — which is why the replay is distinguishable from a
// retry rather than looking like one more attempt.
func (s *Service) Replay(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.Delivery, error) {
	var row contracts.Delivery
	res := tx.DB().Table(deliveryTable).Where("id = ?", id).Limit(1).Take(&row)
	if res.Error != nil {
		return nil, crud.Classify(res.Error)
	}
	// The endpoint is the aggregate root: the thing a concurrent disablement or
	// rotate is also deciding about, and the thing whose status decides whether
	// a replay could ever be sent.
	endpoint, err := crud.GetForUpdate[*contracts.Endpoint](tx, row.EndpointID)
	if err != nil {
		return nil, err
	}
	if !endpoint.IsEnabled() {
		return nil, fmt.Errorf("%w: endpoint %s is %s, so nothing can be delivered to it",
			crud.ErrConflict, endpoint.Name, endpoint.Status)
	}
	// A pair with an attempt already waiting is that command having already been
	// performed: answer with the row that is waiting, write nothing, emit nothing.
	// "Waiting" means queued or on the wire, and nothing more: a replay that has
	// already been sent, failed or dropped is an attempt that finished, and an
	// administrator who asks again is asking for another one — answering with the
	// finished row would be rule 9's stale row, and would send nothing.
	//
	// This is a read rather than an INSERT that catches the unique index, and the
	// reason is Postgres': a statement that violates a constraint aborts the
	// transaction, so the recovery would have been issued in a block that no
	// longer accepts commands. The index stays as the backstop for a caller that
	// does not take the lock below at all.
	if waiting := s.waitingReplay(tx, row.EventID, row.EndpointID); waiting != nil {
		return waiting, nil
	}
	// The pair's attempt number continues: a replay of an event that failed four
	// times is the fifth attempt, not the first again.
	var attempt int
	if err := tx.DB().Raw("SELECT COALESCE(MAX(attempt), 0) + 1 FROM "+deliveryTable+" WHERE event_id = ? AND endpoint_id = ?",
		row.EventID, row.EndpointID).Scan(&attempt).Error; err != nil {
		return nil, fmt.Errorf("webhook: number the replay of %s: %w", row.EventID, err)
	}
	at := s.now()
	replayed := &contracts.Delivery{
		Payload: row.Payload, EventID: row.EventID, EventName: row.EventName,
		EndpointID: row.EndpointID, Attempt: attempt, Cause: contracts.CauseReplay,
		State: contracts.StateQueued, AvailableAt: at, Reason: "",
	}
	if err := crud.Create(ctx, tx, replayed); err != nil {
		return nil, err
	}
	return replayed, nil
}

// waitingReplay reports the replay of this pair that is still waiting to be
// answered — the row a previous replay of the same command queued — or nil when
// there is none. A finished replay is deliberately not an answer to a fresh
// request, and the partial unique index covers the same two states this read does,
// so "one waiting replay per pair" is a fact of the schema and of this read at
// once, and a pair whose last replay has been answered is free to be replayed.
//
// A read that fails for any reason other than finding nothing answers nil, which
// is safe: the index is the backstop, and the caller holds the endpoint's row
// lock, so the worst this can do is fail to reuse a row it did not read.
func (s *Service) waitingReplay(tx db.Tx[db.Tenant], eventID, endpointID uuid.UUID) *contracts.Delivery {
	var waiting contracts.Delivery
	res := tx.DB().Table(deliveryTable).
		Where("event_id = ? AND endpoint_id = ? AND cause = 'replay' AND state IN ('queued', 'in_flight')",
			eventID, endpointID).
		Limit(1).Take(&waiting)
	if res.Error != nil {
		return nil
	}
	return &waiting
}

// checkEvents refuses a name this composition does not emit, by name. The
// grammar is contracts' own, so a garbage string is refused even by a composition
// whose catalogue literal has gone stale; the set is the port's.
func (s *Service) checkEvents(list contracts.EventNames) error {
	offered := s.catalogue.EventTypes()
	for _, name := range list {
		if !slices.Contains(offered, name) {
			return fmt.Errorf("%w: %s is not an event this composition emits", crud.ErrInvalid, name)
		}
	}
	return nil
}

func deref(t *time.Time, fallback time.Time) time.Time {
	if t == nil {
		return fallback
	}
	return *t
}
