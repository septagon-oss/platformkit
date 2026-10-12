package internal_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/webhook"
	"github.com/septagon-oss/platformkit/modules/webhook/contracts"
	"github.com/septagon-oss/platformkit/modules/webhook/internal"
)

// The two tenants every isolation case is told apart by. Their ids are random
// rather than fixed: a fixed pair would let a case pass by accident if the
// fixture happened to seed the row into whichever one the assertion ignored.
var (
	acme   = tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}
	globex = tenancy.Tenant{ID: uuid.New(), Slug: "globex", Name: "Globex"}

	emitted = []string{"task.assigned", "task.resolved", "auth.user.invited"}
)

// env is one module instance, one schema, and a clock the three-day rules are
// falsified with. Each case builds its own, so no case sees another's rows.
type env struct {
	t     *testing.T
	conn  *db.Conn
	svc   *internal.Service
	clock *clock
	now   time.Time
}

// clock is the sender's clock. It starts at the real moment, because the
// webhook-timestamp header it stamps has to be one a receiver's 300-second
// window accepts, and steps from there by whole hours for the window rules.
type clock struct {
	mu sync.Mutex
	at time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

// newEnv builds the module. roots may be nil, which is the platform's own trust
// store; allowLocal is the deployment flag, and the httptest receivers are on
// 127.0.0.1, so a case that sends sets it.
func newEnv(t *testing.T, allowLocal bool, roots *x509.CertPool, alerts contracts.Alerts) *env {
	t.Helper()
	_, conn := dbtest.Schema(t, webhook.Migrations)
	at := time.Now().UTC().Truncate(time.Microsecond)
	c := &clock{at: at}
	svc := internal.NewService(internal.Deps{
		SecretKey: make([]byte, 32),
		Catalogue: catalogue(emitted),
		Alerts:    alerts,
		Policy:    internal.AddressPolicy{AllowLocal: allowLocal, Roots: roots},
		Now:       c.Now,
	})
	return &env{t: t, conn: conn, svc: svc, clock: c, now: at}
}

// catalogue is the port the composition writes: the names this one emits.
type catalogue []string

func (c catalogue) EventTypes() []string { return c }

// lister is jobs.TenantLister for the tenants a case named.
type lister []tenancy.Tenant

func (l lister) List(context.Context, db.Tx[db.System]) ([]tenancy.Tenant, error) { return l, nil }

// in runs fn inside one tenant's transaction, the way a request handler does,
// and commits.
func (e *env) in(tenant tenancy.Tenant, actor uuid.UUID, fn func(ctx context.Context, tx db.Tx[db.Tenant]) error) {
	e.t.Helper()
	ctx := tenancy.WithTenant(context.Background(), tenant)
	if actor != uuid.Nil {
		ctx = tenancy.WithActor(ctx, actor)
	}
	if err := db.Run(ctx, e.conn, fn); err != nil {
		e.t.Fatalf("in %s: %v", tenant.Slug, err)
	}
}

// register is the create route's body: an endpoint with an address and a list.
func (e *env) register(tenant tenancy.Tenant, actor uuid.UUID, url string, list ...string) (*contracts.Endpoint, string) {
	e.t.Helper()
	var issued *contracts.Issued
	e.in(tenant, actor, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		issued, err = e.svc.Create(ctx, tx, &contracts.Endpoint{
			Name: "receiver", URL: url, Events: contracts.EventNames(list),
		})
		return err
	})
	if issued == nil {
		e.t.Fatalf("registering on %s: nothing came back", tenant.Slug)
	}
	return issued.Endpoint, issued.Secret
}

// fanout is the outbox handler, run as the event's tenant.
func (e *env) fanout(tenant tenancy.Tenant, ev events.Event) {
	e.t.Helper()
	e.in(tenant, uuid.Nil, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return e.svc.Fanout(ctx, tx, ev)
	})
}

// drain is one tick of the delivery job.
func (e *env) drain(tenants ...tenancy.Tenant) {
	e.t.Helper()
	job := internal.Deliver(e.svc, lister(tenants), time.Minute, 5, 2*time.Second, time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := job.Run(ctx, e.conn); err != nil {
		e.t.Fatalf("deliver: %v", err)
	}
}

// event is a CloudEvent of one type, for the fan-out to match or ignore.
func event(tenant tenancy.Tenant, name string, at time.Time) events.Event {
	body, err := json.Marshal(map[string]any{"taskId": uuid.New().String(), "at": at.UTC().Format(time.RFC3339Nano)})
	if err != nil {
		panic(err)
	}
	return events.Event{
		ID: uuid.New(), Name: name, TenantID: tenant.ID, At: at.UTC(), Payload: body,
	}
}

// deliveries is this tenant's log, oldest first — read through row level
// security, which is what makes the other tenant's rows invisible here rather
// than merely absent from a query.
func (e *env) deliveries(tenant tenancy.Tenant, endpoint uuid.UUID) []*contracts.Delivery {
	e.t.Helper()
	var rows []*contracts.Delivery
	e.in(tenant, uuid.Nil, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Table("webhook_deliveries").Where("endpoint_id = ?", endpoint).
			Order("seq").Find(&rows).Error
	})
	return rows
}

// outboxNames is what one tenant's transaction published, which is how the
// "emits nothing" half of a refusal is checked.
func (e *env) outboxNames(tenant tenancy.Tenant) []string {
	e.t.Helper()
	var names []string
	e.in(tenant, uuid.Nil, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT name FROM platformkit_outbox WHERE tenant_id = ? ORDER BY created_at, id`, tenant.ID).
			Scan(&names).Error
	})
	return names
}

func state(rows []*contracts.Delivery) []string {
	out := make([]string, len(rows))
	for i, d := range rows {
		out[i] = fmt.Sprintf("%s/%s/%d", d.Cause, d.State, d.Attempt)
	}
	return out
}

// received is one POST a receiver got, with the three headers and the body it
// arrived with.
type received struct {
	id, timestamp, signature, delivery string
	body                               []byte
}

// receiver is the customer's end: an httptest server on loopback, TLS, that
// answers with whatever answers says and remembers what it was sent.
type receiver struct {
	server   *httptest.Server
	mu       sync.Mutex
	answers  []int
	calls    []received
	requests int
}

func newReceiver(t *testing.T, answers ...int) *receiver {
	t.Helper()
	r := &receiver{answers: answers}
	r.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body := make([]byte, req.ContentLength)
		_, _ = req.Body.Read(body)
		n := r.next()
		r.mu.Lock()
		r.requests++
		r.calls = append(r.calls, received{
			id: req.Header.Get("webhook-id"), timestamp: req.Header.Get("webhook-timestamp"),
			signature: req.Header.Get("webhook-signature"), delivery: req.Header.Get("x-platformkit-delivery"),
			body: body,
		})
		r.mu.Unlock()
		w.WriteHeader(n)
	}))
	t.Cleanup(r.server.Close)
	return r
}

func (r *receiver) next() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.answers) == 0 {
		return http.StatusOK
	}
	if len(r.answers) == 1 {
		return r.answers[0]
	}
	n := r.answers[0]
	r.answers = r.answers[1:]
	return n
}

func (r *receiver) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.requests
}

func (r *receiver) last(t *testing.T) received {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.calls) == 0 {
		t.Fatalf("the receiver was never called")
	}
	return r.calls[len(r.calls)-1]
}

// TestASubscriberReceivesASignedEvent is the brief's first acceptance case: a
// signed event arrives at an httptest endpoint and the signature verifies — with
// the standard library, over id + "." + timestamp + "." + body, which is the
// check the module's README tells a receiver to write.
func TestASubscriberReceivesASignedEvent(t *testing.T) {
	to := newReceiver(t, http.StatusOK)
	e := newEnv(t, true, pool(to.server.Certificate()), nil)
	who := uuid.New()
	endpoint, secret := e.register(acme, who, to.server.URL+"/hooks", "task.assigned")

	at := e.now
	e.fanout(acme, event(acme, "task.assigned", at))
	if rows := e.deliveries(acme, endpoint.ID); len(rows) != 1 || rows[0].State != contracts.StateQueued {
		t.Fatalf("the fan-out wrote %v, wanted one queued row", state(e.deliveries(acme, endpoint.ID)))
	}
	e.drain(acme)

	if to.count() != 1 {
		t.Fatalf("the receiver saw %d requests, wanted 1 (log: %v)", to.count(), state(e.deliveries(acme, endpoint.ID)))
	}
	got := to.last(t)
	if got.id == "" || got.timestamp == "" || got.signature == "" {
		t.Fatalf("the three headers are the contract: %+v", got)
	}
	if !verify(t, secret, got.id, got.timestamp, got.body, got.signature) {
		t.Fatalf("the signature does not verify over the body the receiver was given: %q", got.signature)
	}
	if !strings.HasPrefix(secret, "whsec_") {
		t.Fatalf("a secret Standard Webhooks' own library would not accept: %q", secret)
	}
	// The body is the CloudEvent as published, and the signed bytes are the
	// bytes that arrived.
	var doc map[string]any
	if err := json.Unmarshal(got.body, &doc); err != nil {
		t.Fatalf("the body is not the JSON document it claims: %v", err)
	}
	if doc["type"] != "task.assigned" || doc["id"] != got.id {
		t.Fatalf("the body is not the CloudEvent that was published: %v", doc["type"])
	}
	if got.delivery == "" {
		t.Fatal("a delivery row's id is the ticket a support call names")
	}
	rows := e.deliveries(acme, endpoint.ID)
	if len(rows) != 1 || rows[0].State != contracts.StateSent || rows[0].StatusCode != 200 {
		t.Fatalf("the log reads %v, wanted one sent row answering 200", state(rows))
	}
}

// TestARetryAfterAFiveHundredIsDelivered is the brief's second acceptance case,
// and it pins the ladder as a table at the same time: the second attempt is
// available exactly one minute after the first, because that is rung 1 and a
// formula a test cannot name would be a formula nobody read.
func TestARetryAfterAFiveHundredIsDelivered(t *testing.T) {
	to := newReceiver(t, http.StatusInternalServerError, http.StatusOK)
	e := newEnv(t, true, pool(to.server.Certificate()), nil)
	endpoint, _ := e.register(acme, uuid.New(), to.server.URL, "task.assigned")

	e.fanout(acme, event(acme, "task.assigned", e.now))
	e.drain(acme)
	rows := e.deliveries(acme, endpoint.ID)
	if len(rows) != 2 {
		t.Fatalf("a 500 wrote %v, wanted a failed attempt and its retry", state(rows))
	}
	if rows[0].State != contracts.StateFailed || rows[0].StatusCode != 500 || rows[1].State != contracts.StateQueued {
		t.Fatalf("the ladder's first two rows read %v", state(rows))
	}
	// Both ends of the gap are the module's own clock: attempted_at is what the
	// sender stamped when it opened the socket, and available_at is that plus the
	// rung. Compared against the row's available_at — the database's transaction
	// time — the same assertion would be off by the milliseconds between the two
	// clocks, and a check that is only nearly true is a check that fails on CI.
	if gap := rows[1].AvailableAt.Sub(*rows[0].AttemptedAt); gap != time.Minute {
		t.Fatalf("rung 1 is exactly a minute, and this gap was %s", gap)
	}
	if rows[1].Cause != contracts.CauseRetry || rows[1].Attempt != 2 {
		t.Fatalf("the retry must be readable as a retry in the log: %v", state(rows)[1])
	}
	// The same payload: a retry that re-marshalled the event would sign a
	// different string than the one its first attempt signed.
	if string(rows[0].Payload) != string(rows[1].Payload) {
		t.Fatal("the retry signs bytes the first attempt never sent")
	}

	// The row is not due yet: the ladder is a promise and not a suggestion.
	e.drain(acme)
	if to.count() != 1 {
		t.Fatalf("the retry ran %s before its rung: %d requests", rows[1].AvailableAt.Sub(e.clock.Now()), to.count())
	}

	e.clock.Add(time.Minute)
	e.drain(acme)
	if to.count() != 2 {
		t.Fatalf("the retry after the 500 never happened: %d requests, log %v", to.count(), state(e.deliveries(acme, endpoint.ID)))
	}
	rows = e.deliveries(acme, endpoint.ID)
	if rows[1].State != contracts.StateSent || rows[1].StatusCode != 200 {
		t.Fatalf("the retry ended as %v", state(rows))
	}
}

// TestAFourHundredFourIsNotRetried: an answer the receiver will keep giving is
// not a fault. One dropped row, no successor, and the reason names the status.
func TestAFourHundredFourIsNotRetried(t *testing.T) {
	to := newReceiver(t, http.StatusNotFound)
	e := newEnv(t, true, pool(to.server.Certificate()), nil)
	endpoint, _ := e.register(acme, uuid.New(), to.server.URL, "task.assigned")

	e.fanout(acme, event(acme, "task.assigned", e.now))
	e.drain(acme)
	e.clock.Add(24 * time.Hour)
	e.drain(acme)

	rows := e.deliveries(acme, endpoint.ID)
	if len(rows) != 1 || rows[0].State != contracts.StateDropped {
		t.Fatalf("a 404 wrote %v, wanted one dropped row and no successor", state(rows))
	}
	if !strings.Contains(rows[0].Reason, "Not Found") {
		t.Fatalf("the reason names nothing a reader can act on: %q", rows[0].Reason)
	}
	if to.count() != 1 {
		t.Fatalf("a 404 was retried %d times", to.count())
	}
}

// TestAnotherTenantsEndpointNeverHearsTheEvent is the brief's third acceptance
// case, and it is the RLS half rather than a WHERE clause: acme's event is
// fanned out inside acme's transaction, and globex — which registered an
// endpoint for the very same event type — gets no row, no socket and no read of
// acme's rows.
func TestAnotherTenantsEndpointNeverHearsTheEvent(t *testing.T) {
	mine, theirs := newReceiver(t, http.StatusOK), newReceiver(t, http.StatusOK)
	e := newEnv(t, true, pool(mine.server.Certificate(), theirs.server.Certificate()), nil)

	mineEndpoint, _ := e.register(acme, uuid.New(), mine.server.URL, "task.assigned")
	theirsEndpoint, _ := e.register(globex, uuid.New(), theirs.server.URL, "task.assigned")

	e.fanout(acme, event(acme, "task.assigned", e.now))
	e.drain(acme, globex)

	if theirs.count() != 0 {
		t.Fatalf("another tenant's endpoint received the event %d times", theirs.count())
	}
	if mine.count() != 1 {
		t.Fatalf("the right endpoint received it %d times", mine.count())
	}
	if rows := e.deliveries(globex, theirsEndpoint.ID); len(rows) != 0 {
		t.Fatalf("globex's tenant holds %v for an event of acme's", state(rows))
	}
	// And the row it cannot write, it cannot read either: the id travels, the
	// policy decides, and the answer is the same as for a row that never was.
	e.in(globex, uuid.Nil, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := crud.Get[*contracts.Endpoint](tx, mineEndpoint.ID); err == nil {
			t.Errorf("globex read acme's endpoint by id")
		} else if !isNotFound(err) {
			t.Errorf("the refusal for a foreign row is ErrNotFound, and this was %v", err)
		}
		if _, err := crud.Get[*contracts.Delivery](tx, e.deliveries(acme, mineEndpoint.ID)[0].ID); err == nil {
			t.Errorf("globex read acme's delivery row by id")
		}
		return nil
	})
}

// TestAnEventNobodySubscribedToIsSilence: the fan-out's ordinary answer is no
// rows at all, and an error there would put every unsubscribed event in the
// dead-letter queue.
func TestAnEventNobodySubscribedToIsSilence(t *testing.T) {
	to := newReceiver(t, http.StatusOK)
	e := newEnv(t, true, pool(to.server.Certificate()), nil)
	endpoint, _ := e.register(acme, uuid.New(), to.server.URL, "task.assigned")

	e.fanout(acme, event(acme, "task.resolved", e.now))
	if rows := e.deliveries(acme, endpoint.ID); len(rows) != 0 {
		t.Fatalf("an event nobody listed wrote %v", state(rows))
	}
	e.drain(acme)
	if to.count() != 0 {
		t.Fatalf("a socket was opened for an event no endpoint listed")
	}
}

// TestTheMetadataAddressIsRefusedWhateverTheFlagSays: the one refusal that
// survives the deployment's own switch. AllowLocalEndpoints says "this
// deployment dials a local receiver", not "this deployment POSTs to its own
// cloud provider's credentials endpoint".
func TestTheMetadataAddressIsRefusedWhateverTheFlagSays(t *testing.T) {
	e := newEnv(t, true, nil, nil)
	for _, url := range []string{"https://169.254.169.254/latest/meta-data", "https://[fe80::1]/hook", "https://0.0.0.0/hook"} {
		err := e.createErr(acme, uuid.New(), url)
		if err == nil {
			t.Errorf("%s was registered", url)
			continue
		}
		if !isInvalid(err) {
			t.Errorf("%s: the refusal should name the value, and this was %v", url, err)
		}
	}
}

// TestAPrivateAddressIsRefusedUntilTheDeploymentSaysSo pins both halves of the
// flag: a tenant cannot register a private address, and the deployment's switch
// — which no column, field or route carries — is what opens it.
func TestAPrivateAddressIsRefusedUntilTheDeploymentSaysSo(t *testing.T) {
	locked := newEnv(t, false, nil, nil)
	if err := locked.createErr(acme, uuid.New(), "https://10.40.0.7/hook"); err == nil {
		t.Error("a private address was registered while the deployment forbids it")
	} else if !strings.Contains(err.Error(), "public") {
		t.Errorf("the refusal does not name the rule: %v", err)
	}
	opened := newEnv(t, true, nil, nil)
	if err := opened.createErr(acme, uuid.New(), "https://10.40.0.7/hook"); err != nil {
		t.Errorf("the deployment's own flag should open the private range: %v", err)
	}
	// And a name that resolves to loopback is the same decision, made on the
	// answer rather than the string: this is the case the write cannot make, so
	// the attempt carries it.
	names := newEnv(t, false, nil, nil)
	endpoint, _ := names.register(acme, uuid.New(), "https://localhost:8443/hook", "task.assigned")
	names.fanout(acme, event(acme, "task.assigned", names.now))
	names.drain(acme)
	rows := names.deliveries(acme, endpoint.ID)
	if len(rows) != 1 || rows[0].State != contracts.StateDropped {
		t.Fatalf("an address the gate refuses should be dropped, and the log read %v", state(rows))
	}
	if !strings.Contains(rows[0].Reason, "public") {
		t.Errorf("the log does not say which rule refused: %q", rows[0].Reason)
	}
	if rows[0].Attempt != 1 || len(rows) != 1 {
		t.Errorf("a refused address was retried: %v", state(rows))
	}
}

// TestAnEndpointThatFailedForThreeDaysIsDisabledOnce is the fault rule: the
// clock counts from the *first* failure, the transition publishes one event and
// one notice, and a second failure after it publishes neither.
// TestAReplayIsDistinguishableAndIdempotent: an administrator's replay is
// cause='replay' and continues the attempt number, and performing it twice writes
// nothing, emits nothing and answers with the row the first one wrote.
func TestAReplayIsDistinguishableAndIdempotent(t *testing.T) {
	to := newReceiver(t, http.StatusOK)
	e := newEnv(t, true, pool(to.server.Certificate()), nil)
	endpoint, _ := e.register(acme, uuid.New(), to.server.URL, "task.assigned")
	e.fanout(acme, event(acme, "task.assigned", e.now))
	first := e.deliveries(acme, endpoint.ID)[0]

	var replay *contracts.Delivery
	e.in(acme, uuid.New(), func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		replay, err = e.svc.Replay(ctx, tx, first.ID)
		return err
	})
	if replay.Cause != contracts.CauseReplay {
		t.Fatalf("a replay is written as %q, and the log has to tell it from a retry", replay.Cause)
	}
	if replay.Attempt != 2 {
		t.Fatalf("a replay continues the count: attempt %d", replay.Attempt)
	}
	before := e.outboxNames(acme)

	var again *contracts.Delivery
	e.in(acme, uuid.New(), func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		again, err = e.svc.Replay(ctx, tx, first.ID)
		return err
	})
	if again.ID != replay.ID {
		t.Fatalf("the second replay wrote a row: %s against %s", again.ID, replay.ID)
	}
	if len(e.deliveries(acme, endpoint.ID)) != 2 {
		t.Fatalf("the log grew: %v", state(e.deliveries(acme, endpoint.ID)))
	}
	if after := e.outboxNames(acme); !slices.Equal(before, after) {
		t.Fatalf("a mutation that wrote nothing emitted something: %v against %v", after, before)
	}
}

// TestAReplayAgainstADisabledEndpointWritesAndEmitsNothing is rule 9 on the one
// command this module has beyond the CRUD routes.
func TestAReplayAgainstADisabledEndpointWritesAndEmitsNothing(t *testing.T) {
	to := newReceiver(t, http.StatusOK)
	e := newEnv(t, true, pool(to.server.Certificate()), nil)
	endpoint, _ := e.register(acme, uuid.New(), to.server.URL, "task.assigned")
	e.fanout(acme, event(acme, "task.assigned", e.now))
	first := e.deliveries(acme, endpoint.ID)[0]
	before := e.outboxNames(acme)

	who := uuid.New()
	e.in(acme, who, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := e.svc.Disable(ctx, tx, endpoint.ID)
		return err
	})
	afterDisable := e.outboxNames(acme)

	err := e.inErr(acme, who, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := e.svc.Replay(ctx, tx, first.ID)
		return err
	})
	if err == nil {
		t.Fatal("a replay against a disabled endpoint was accepted")
	}
	if !isConflict(err) {
		t.Errorf("the refusal should be a conflict naming the state, and this was %v", err)
	}
	if names := e.outboxNames(acme); !slices.Equal(names, afterDisable) {
		t.Errorf("the refused replay emitted: %v against %v", names, afterDisable)
	}
	if !slices.Contains(afterDisable, contracts.EventEndpointDisabled) {
		t.Errorf("the disablement published nothing: %v against %v", afterDisable, before)
	}
	if len(afterDisable) != len(before)+1 {
		t.Errorf("the disablement published more than its own one event: %v against %v", afterDisable, before)
	}
}

// TestASecretAppearsInNoRead: the entity, its JSON, and the audit payload the
// create published. The whsec_ value exists in two responses and in no read.
func TestASecretAppearsInNoRead(t *testing.T) {
	e := newEnv(t, false, nil, nil)
	who := uuid.New()
	endpoint, secret := e.register(acme, who, "https://hooks.example.com/pk", "task.assigned")
	if secret == "" || !strings.HasPrefix(secret, "whsec_") {
		t.Fatalf("the create answered %q", secret)
	}

	var read *contracts.Endpoint
	e.in(acme, who, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		read, err = crud.Get[*contracts.Endpoint](tx, endpoint.ID)
		return err
	})
	body, err := json.Marshal(read)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "whsec") || strings.Contains(string(body), strings.TrimPrefix(secret, "whsec_")) {
		t.Fatalf("the secret is readable on the entity: %s", body)
	}
	names := e.outboxNames(acme)
	if !slices.Contains(names, contracts.EventEndpointCreated) {
		t.Fatalf("the create published %v", names)
	}
	var payloads []string
	e.in(acme, uuid.Nil, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT payload::text FROM platformkit_outbox WHERE name = ?`, contracts.EventEndpointCreated).
			Scan(&payloads).Error
	})
	for _, p := range payloads {
		if strings.Contains(p, "whsec") {
			t.Fatalf("the audit trail carries the secret: %s", p)
		}
	}
}

// TestRotationReplacesTheSecret: a rotate answers with a new whsec_ value once,
// the endpoint's deliveries sign with it from then on, and the value never
// appears in a read of anything.
func TestRotationReplacesTheSecret(t *testing.T) {
	to := newReceiver(t, http.StatusOK, http.StatusOK)
	e := newEnv(t, true, pool(to.server.Certificate()), nil)
	who := uuid.New()
	endpoint, first := e.register(acme, who, to.server.URL, "task.assigned")

	var rotated *contracts.Issued
	e.in(acme, who, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		rotated, err = e.svc.Rotate(ctx, tx, endpoint.ID)
		return err
	})
	if rotated.Secret == first || !strings.HasPrefix(rotated.Secret, "whsec_") {
		t.Fatalf("rotating answered %q, which is the secret it replaced", rotated.Secret)
	}
	e.fanout(acme, event(acme, "task.assigned", e.now))
	e.drain(acme)
	got := to.last(t)
	if !verify(t, rotated.Secret, got.id, got.timestamp, got.body, got.signature) {
		t.Fatal("after a rotation the deliveries still sign with the old secret")
	}
	if verify(t, first, got.id, got.timestamp, got.body, got.signature) {
		t.Fatal("the rotated-out secret still signs, so a leaked one is a live one")
	}
}

// TestAFailedEndpointKeepsItsHistory, and its queued attempts end with a reason.
func TestAFailedEndpointKeepsItsHistory(t *testing.T) {
	to := newReceiver(t, http.StatusOK)
	e := newEnv(t, true, pool(to.server.Certificate()), nil)
	who := uuid.New()
	endpoint, _ := e.register(acme, who, to.server.URL, "task.assigned")
	e.fanout(acme, event(acme, "task.assigned", e.now))
	e.drain(acme)
	before := e.deliveries(acme, endpoint.ID)

	e.in(acme, who, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := e.svc.Disable(ctx, tx, endpoint.ID)
		return err
	})
	after := e.deliveries(acme, endpoint.ID)
	if len(after) != len(before) {
		t.Fatalf("a disablement removed %d rows of history", len(before)-len(after))
	}
	for _, d := range after {
		if d.State == contracts.StateQueued {
			t.Errorf("a queued attempt survived the disablement unexplained: %v", state(after))
		}
	}
}

// inErr is env.in with the failure returned instead of fatal, for the cases whose
// whole subject is a refusal.
func (e *env) inErr(tenant tenancy.Tenant, actor uuid.UUID, fn func(ctx context.Context, tx db.Tx[db.Tenant]) error) error {
	e.t.Helper()
	ctx := tenancy.WithTenant(context.Background(), tenant)
	if actor != uuid.Nil {
		ctx = tenancy.WithActor(ctx, actor)
	}
	return db.Run(ctx, e.conn, fn)
}

// createErr is the create command's refusal, or nil.
func (e *env) createErr(tenant tenancy.Tenant, actor uuid.UUID, url string) error {
	e.t.Helper()
	return e.inErr(tenant, actor, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := e.svc.Create(ctx, tx, &contracts.Endpoint{
			Name: "receiver", URL: url, Events: contracts.EventNames{"task.assigned"},
		})
		return err
	})
}

// pool trusts the two test receivers' own certificates. The module verifies TLS
// against the platform roots in every deployment; a test has no choice but to
// hand it the certificate the server it started was made with.
func pool(certs ...*x509.Certificate) *x509.CertPool {
	out := x509.NewCertPool()
	for _, c := range certs {
		out.AddCert(c)
	}
	return out
}

func mustInt64(t *testing.T, s string) int64 {
	t.Helper()
	var n int64
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		t.Fatalf("webhook-timestamp is unix seconds, and this was %q: %v", s, err)
	}
	return n
}

func isNotFound(err error) bool { return errors.Is(err, crud.ErrNotFound) }
func isConflict(err error) bool { return errors.Is(err, crud.ErrConflict) }
func isInvalid(err error) bool  { return errors.Is(err, crud.ErrInvalid) }

// verify is the receiver's check, written here from the README's twelve lines
// rather than called from the module: a test that verified a signature with the
// same function that signed it would prove nothing at all. It is the check the
// reference Standard Webhooks library writes — split the header on spaces, split
// each part on its first comma, accept the first v1 entry whose base64 HMAC
// matches — and not a mirror of the module's own header builder, which is exactly
// how a wrong separator in the header survived its own suite once.
func verify(t *testing.T, secret, id, timestamp string, body []byte, header string) bool {
	t.Helper()
	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_"))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(id + "." + timestamp + "."))
	_, _ = mac.Write(body)
	want := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	at, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || abs(time.Now().Unix()-at) > 300 {
		return false
	}
	for _, entry := range strings.Split(header, " ") {
		parts := strings.SplitN(entry, ",", 2)
		if len(parts) != 2 || parts[0] != "v1" {
			continue
		}
		if hmac.Equal([]byte(parts[1]), []byte(want)) {
			return true
		}
	}
	return false
}

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// TestTwoReplaysOfOnePairSettleOnOneRow is the concurrency case: two commands on
// one row, in two transactions, at once. Both are legitimate — the endpoint is
// enabled, the caller holds the grant — and the answer has to be one queued
// attempt either way, which is what the endpoint's row lock and the partial
// unique index are for. Run under -race, it is also the case that would show a
// torn read of the attempt number.
func TestTwoReplaysOfOnePairSettleOnOneRow(t *testing.T) {
	to := newReceiver(t, http.StatusOK)
	e := newEnv(t, true, pool(to.server.Certificate()), nil)
	endpoint, _ := e.register(acme, uuid.New(), to.server.URL, "task.assigned")
	e.fanout(acme, event(acme, "task.assigned", e.now))
	first := e.deliveries(acme, endpoint.ID)[0]

	ids := make(chan uuid.UUID, 2)
	for range 2 {
		go func() {
			var id uuid.UUID
			err := e.inErr(acme, uuid.New(), func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				row, err := e.svc.Replay(ctx, tx, first.ID)
				if err == nil {
					id = row.ID
				}
				return err
			})
			if err != nil {
				t.Errorf("a concurrent replay was refused: %v", err)
			}
			ids <- id
		}()
	}
	a, b := <-ids, <-ids
	if a != b {
		t.Fatalf("two replays of one pair wrote two rows: %s and %s", a, b)
	}
	rows := e.deliveries(acme, endpoint.ID)
	if len(rows) != 2 {
		t.Fatalf("the log grew past one initial and one replay: %v", state(rows))
	}
	if got := e.outboxNames(acme); len(got) != 1 {
		t.Errorf("the pair published %v, wanted only the create", got)
	}
}
