// Package stream lets a live connection watch the events an application emits.
//
// A connection is a notice and never a payload: each frame names which event
// happened and when, and the client's next read is the truth. That single rule is
// what lets the whole feature sit beside the events transport instead of in front
// of every read path, and it is why a frame carries no id a server would have to
// honour — a connection that misses notices is told so (resync) rather than given
// a position to resume from, which this kernel does not keep.
//
// The tenant boundary is the address. A watch is opened for one tenant through
// transport.Transport.Observe, whose filter is that tenant's exact subject, and the
// transport compares the address it routed by against the document before the
// notice is handed over. Nothing here filters another tenant's events out of a
// stream, because no other tenant's event ever arrives on the watch to filter.
//
// Nothing here is a per-process singleton: the watches, the per-tenant count and
// the per-person count live in the Hub one composition builds, so two compositions
// in one process get two hubs. What is deliberately per process is the two
// connection ceilings themselves — the README names that, and a cluster-wide cap
// is the follow-up it names beside it.
package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events/transport"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// DefaultMaxPerTenant is how many live connections one tenant may hold in one
// process when the deployment names no number.
const DefaultMaxPerTenant = 500

// Limits bounds one process's live connections. Every member is a number a
// deployment can defend rather than one a select statement remembered, and every
// one is defaulted by DefaultLimits.
type Limits struct {
	// MaxTopics is how many topics one connection may watch.
	MaxTopics int
	// MaxPerPrincipal is how many connections one person may hold in this
	// process in one tenant — a browser opens one per tab. A person in two
	// tenants has two sets of tabs, so the count is theirs per tenant: the
	// ceiling stops one browser opening many streams, it does not stop one
	// account signing in to two.
	MaxPerPrincipal int
	// MaxPerTenant is how many connections one tenant may hold in this process.
	// A cluster of N replicas admits N times this number, which is why it is
	// configurable and why the README says what it is a count of.
	MaxPerTenant int
	// Queue is how many undelivered notices one connection may hold before it is
	// a connection that is not keeping up.
	Queue int
	// WriteDeadline bounds one write. It is armed before the write and taken back
	// when the write and its flush are done, because a deadline is a lifetime: armed
	// once at open it bounds the connection rather than the frame, and left armed it
	// bounds the silence between frames, which is the thing a stream outlives.
	WriteDeadline time.Duration
	// Heartbeat is how often an idle connection says something, and the moment
	// the session and every topic's policy are asked again.
	Heartbeat time.Duration
	// RetryAfter is the wait named on a 429: a slot is freed the moment one of
	// the caller's own tabs closes, so this is a short wait.
	RetryAfter time.Duration
}

// DefaultLimits is the bounds of a connection this deployment said nothing about.
func DefaultLimits() Limits {
	return Limits{
		MaxTopics:       8,
		MaxPerPrincipal: 3,
		MaxPerTenant:    DefaultMaxPerTenant,
		Queue:           64,
		WriteDeadline:   5 * time.Second,
		Heartbeat:       15 * time.Second,
		RetryAfter:      5 * time.Second,
	}
}

// Clock is the connection's idea of now, for the stamps on frames that no event
// caused. nil means time.Now.
type Clock interface{ Now() time.Time }

// Authority answers, for one connection, the two questions the request guards
// asked once: does a session still answer for this caller, and does this caller
// still hold each grant named. It is a port this package declares rather than a
// session or a policy engine it writes: the kernel's session lives behind
// app.Options.Authenticate and its grants behind app.Options.Authorize, and both
// have to be asked inside one short transaction that the composition opens — never
// the request's, which ended when the response began.
//
// An error is the database's or the authorizer's own, and it is not the caller's
// fault: a connection that cannot be re-checked stays up and asks again at the next
// heartbeat, because the alternative is that a four-second outage drops every live
// connection in the tenant.
//
// Every connection is asked, at connect and at every heartbeat, whatever its topics
// carry: asks is empty for one that watches only topics any signed-in person may
// watch, and the answer that matters then is SignedIn. The ctx handed over carries no
// transaction of the request's, so an implementation that opens one with db.Run
// opens one of its own and ends it inside the call.
//
// asks names each distinct permission the topics' policies name, spelled as the
// manifest that defines it spells it, and Refused answers in the same spelling. The
// other half of the route guard's question — whether this tenant may hold the
// permission at all, which module.Permission.Operator answers — is answered by the hub
// from the declaration before any ask leaves: what reaches an implementation is what
// that tenant may exercise, and the composition asks about it as it declares it.
type Authority interface {
	Renew(ctx context.Context, tenant tenancy.Tenant, r *http.Request, asks []string) (Renewal, error)
}

// Renewal is one answer to Authority.Renew.
type Renewal struct {
	// SignedIn is false when no session answers for the caller any more: the
	// connection ends, because there is nobody left to deliver to.
	SignedIn bool
	// Refused names the grants the caller no longer holds. A topic whose policy
	// names one of them is withdrawn; the rest of the connection goes on.
	Refused []string
}

// Deps is one composition's hub: the transport to observe through, the topics the
// manifests declared, the authority that answers again at every heartbeat, and the
// bounds.
type Deps struct {
	Transport transport.Transport
	Registry  *Registry
	Authority Authority
	Limits    Limits
	// Clock may be nil, which means time.Now.
	Clock Clock
	// Log may be nil, which means slog.Default.
	Log *slog.Logger
}

// Hub is one composition's live connections: the watches they share, the slots
// they hold, and the notices on their way.
type Hub struct {
	transport transport.Transport
	registry  *Registry
	authority Authority
	limits    Limits
	clock     Clock
	log       *slog.Logger

	mu           sync.Mutex
	watches      map[watchKey]*watch
	dialling     map[watchKey]chan struct{}
	perTenant    map[uuid.UUID]int
	perPrincipal map[seatKey]int
}

type watchKey struct {
	tenant uuid.UUID
	event  string
}

// seatKey is one person's slots in one tenant. The ceiling counts this person's
// tabs, and a tab is opened inside a tenant: two tenants of one account are two
// browsers' worth of connections, not one that has spent its share.
type seatKey struct {
	tenant uuid.UUID
	user   uuid.UUID
}

// watch is one (tenant, event) observation shared by every connection in the hub
// that asked for it. conns is read under the hub's mutex and never while a notice
// is being handed to anyone.
//
// stop ends the observation and the context it was opened on, so it belongs to the
// watch and not to any one of the connections sharing it: the last one to leave is
// the one that stops it, and the one that happened to open it is only whoever got
// there first.
type watch struct {
	stop  func()
	conns map[*connection]struct{}
}

type connection struct {
	user   uuid.UUID
	tenant tenancy.Tenant
	// request is the one this connection arrived on: the heartbeat re-checks the
	// session of that caller, which is the only caller a stream may speak for.
	request *http.Request
	// topics is the connection's own view of what it still watches, and is guarded
	// by the hub's mutex — the same one the watches are guarded by, because a
	// delivery and a withdrawal have to agree.
	topics map[string]Topic
	// notices is the per-connection queue. It is never closed: a connection that
	// left simply stops reading, which is the whole of what a stop racing a delivery
	// needs, and a send to a closed channel is not an option anyone has to remember.
	notices chan Notice
	// overflowed wakes the writer once per overflow episode; overflow is the flag a
	// second overflow finds already set.
	overflowed chan struct{}
	overflow   atomic.Bool
	// unbounded says this writer has no write deadlines, which is a fact about the
	// writer rather than a failure of it, and is learned once.
	unbounded bool
}

// Notice is one event that happened, as it waits for its connection. It carries
// no payload: the frame format cannot express one, so a field holding one would be
// a field nothing reads.
type Notice struct {
	// ID is the event's own id — the only identity a frame besides its topic.
	ID uuid.UUID
	// Topic is the event's name.
	Topic string
	// At is when the state changed, not when this connection was told.
	At time.Time
}

// New builds the hub of one composition. A missing member is a wiring mistake, so
// it panics where it is written rather than serving a connection that cannot work.
func New(d Deps) *Hub {
	switch {
	case d.Transport == nil:
		panic("stream.New: Deps.Transport is required; a stream with no events transport receives nothing")
	case d.Registry == nil:
		panic("stream.New: Deps.Registry is required; a stream with no topics refuses every topic")
	case d.Authority == nil:
		panic("stream.New: Deps.Authority is required; a connection that is never re-checked outlives its own session")
	}
	limits := d.Limits
	if limits.MaxTopics <= 0 || limits.MaxPerPrincipal <= 0 || limits.MaxPerTenant <= 0 ||
		limits.Queue <= 0 || limits.WriteDeadline <= 0 || limits.Heartbeat <= 0 || limits.RetryAfter <= 0 {
		limits = mergeLimits(limits, DefaultLimits())
	}
	log := d.Log
	if log == nil {
		log = slog.Default()
	}
	clock := d.Clock
	if clock == nil {
		clock = systemClock{}
	}
	return &Hub{
		transport:    d.Transport,
		registry:     d.Registry,
		authority:    d.Authority,
		limits:       limits,
		clock:        clock,
		log:          log,
		watches:      map[watchKey]*watch{},
		dialling:     map[watchKey]chan struct{}{},
		perTenant:    map[uuid.UUID]int{},
		perPrincipal: map[seatKey]int{},
	}
}

// mergeLimits keeps whatever a deployment named and defaults the rest, so one
// configured number never arrives as a zero elsewhere.
func mergeLimits(got, byDefault Limits) Limits {
	out := byDefault
	if got.MaxTopics > 0 {
		out.MaxTopics = got.MaxTopics
	}
	if got.MaxPerPrincipal > 0 {
		out.MaxPerPrincipal = got.MaxPerPrincipal
	}
	if got.MaxPerTenant > 0 {
		out.MaxPerTenant = got.MaxPerTenant
	}
	if got.Queue > 0 {
		out.Queue = got.Queue
	}
	if got.WriteDeadline > 0 {
		out.WriteDeadline = got.WriteDeadline
	}
	if got.Heartbeat > 0 {
		out.Heartbeat = got.Heartbeat
	}
	if got.RetryAfter > 0 {
		out.RetryAfter = got.RetryAfter
	}
	return out
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

// Request is one connection's ask: whose it is, and what it asked to watch. The
// tenant and the principal are the request's own, resolved by the guards — a topic
// is an event name and nothing on this address can name a tenant.
type Request struct {
	Tenant    tenancy.Tenant
	Principal tenancy.Principal
	HTTP      *http.Request
	// Topics is what the caller asked for, in whatever spelling: one member per
	// topic, or one member holding a comma-separated list. Serve normalises it.
	Topics []string
}

// The reason codes a stream refusal is answered with, in the shape every other
// refusal in this kernel wears: "CODE: the sentence". A code is what makes a refusal
// findable by something other than its prose — ui/page's catalogue looks a refusal's
// sentence up by code, and one that is only a sentence is a sentence in one language.
// These three name this endpoint's own verdicts; the three that answer a guard's own
// question reuse that guard's code (httpx.CodeAnonymous, CodeDenied,
// CodeLimitExhausted), because the refusal is the same answer arrived at again.
//
// None of the three has a row in that catalogue, for the reason two of httpx's codes
// do not either: every sentence they carry names something the caller has to have —
// the topic, the count, the ceiling — and the translation mechanism replaces a
// sentence rather than interpolating an argument, so a translated one would drop it.
const (
	// CodeAskInvalid is an ask this endpoint cannot answer as written: a topic that
	// is not an event name, no topic at all, or more topics than one connection may
	// hold. What is wrong is in the caller's own request.
	CodeAskInvalid = "STREAM_ASK_INVALID"
	// CodeTopicUndeclared is a topic no composed module declares a stream for. It
	// names the topic it could not find, which is the difference between this
	// refusal and one a caller can act on.
	CodeTopicUndeclared = "STREAM_TOPIC_UNDECLARED"
	// CodeUnavailable is this seam's own fault rather than the caller's: a writer
	// that cannot flush, a broker that would not take the observation, an authority
	// that could not answer this caller's question. The caller did nothing wrong, so
	// the sentence says what will not work rather than what to change.
	CodeUnavailable = "STREAM_UNAVAILABLE"
)

// Refusal is a connection the hub would not take, and the answer it owes: the
// status, the sentence for the problem document (its own reason code first, the
// way every other refusal here is written), and the wait when the answer is
// that the tenant or the person is at their ceiling.
//
// A Refusal is returned only while nothing of the response has reached the wire,
// which is what makes it an answer rather than an event: a failure after the first
// frame is an ordinary error, and the caller has no response left to write.
type Refusal struct {
	Status     int
	Detail     string
	RetryAfter time.Duration
}

func (r *Refusal) Error() string { return r.Detail }

// Serve runs one connection until it ends: it refuses what it will not accept
// before a byte, opens the watches it shares with the other connections watching
// the same (tenant, event), writes the opening frames, and then forwards notices
// until the client goes away, the session ends, a write fails, or the last topic
// is withdrawn.
//
// It is the whole of the feature in one method on purpose: a caller that held a
// reservation, then a streamer, would be a caller that could leave a slot held.
// Every counter, every watch and every stop is released by this function's defer,
// whatever the reason it is returning.
func (h *Hub) Serve(ctx context.Context, w http.ResponseWriter, req Request) error {
	// First, because a response that cannot flush is a deployment fault and
	// because it asks nothing of the request: nothing is written to learn it.
	if !httpx.CanStream(w) {
		h.log.ErrorContext(ctx, "stream: the response cannot be flushed, so no stream begins",
			"tenant", req.Tenant.ID, "user", req.Principal.UserID, "writer", fmt.Sprintf("%T", w))
		return &Refusal{Status: http.StatusInternalServerError,
			Detail: CodeUnavailable + ": this response cannot be streamed: the writer in front of it cannot be flushed"}
	}
	if req.HTTP == nil {
		return &Refusal{Status: http.StatusInternalServerError,
			Detail: CodeUnavailable + ": the stream has no request to re-check its session against"}
	}
	names, bad := normalise(req.Topics)
	switch {
	case bad != "":
		return &Refusal{Status: http.StatusBadRequest,
			Detail: CodeAskInvalid + fmt.Sprintf(": %q is not an event name", bad)}
	case len(names) == 0:
		return &Refusal{Status: http.StatusBadRequest, Detail: CodeAskInvalid + ": a connection watches at least one topic"}
	case len(names) > h.limits.MaxTopics:
		return &Refusal{Status: http.StatusBadRequest,
			Detail: CodeAskInvalid + fmt.Sprintf(": a connection watches at most %d topics; %d were asked for", h.limits.MaxTopics, len(names))}
	}
	topics := make([]Topic, 0, len(names))
	for _, name := range names {
		t, ok := h.registry.Topic(name)
		if !ok {
			h.refused(ctx, req, names, "the topic is not streamed here")
			return &Refusal{Status: http.StatusForbidden,
				Detail: CodeTopicUndeclared + fmt.Sprintf(": the topic %s is not streamed by this application", name)}
		}
		topics = append(topics, t)
	}
	sctx, cancel := context.WithCancel(db.Detached(ctx))
	defer cancel()

	if err := h.admit(sctx, req, topics); err != nil {
		h.refused(ctx, req, names, "the policy refused")
		return err
	}

	c, err := h.register(req, topics)
	if err != nil {
		h.refused(ctx, req, names, "the connection ceiling")
		return err
	}
	defer h.release(c)

	for _, t := range topics {
		if err := h.watchTopic(sctx, c, t); err != nil {
			h.log.ErrorContext(ctx, "stream: the events transport would not observe a topic",
				"tenant", req.Tenant.ID, "user", req.Principal.UserID, "topic", t.Event, "error", err)
			return &Refusal{Status: http.StatusInternalServerError,
				Detail: CodeUnavailable + fmt.Sprintf(": this application cannot watch %s right now", t.Event)}
		}
	}

	rc := http.NewResponseController(w)
	if err := h.open(w, rc, c); err != nil {
		return err
	}
	return h.loop(sctx, rc, w, c)
}

// normalise turns whatever the query said into event names: every member split on
// commas, every part trimmed, empties and repeats gone, and the first part that is
// not an event name named rather than dropped. Both spellings of a repeated
// parameter — ?topics=a&topics=b and ?topics=a,b — mean the same thing.
func normalise(raw []string) (names []string, bad string) {
	seen := map[string]bool{}
	for _, member := range raw {
		for _, part := range strings.Split(member, ",") {
			name := strings.TrimSpace(part)
			if name == "" {
				continue
			}
			if !transport.ValidName(name) {
				return nil, name
			}
			if seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
	}
	return names, ""
}

// admit asks, before any byte, what the topics themselves cannot answer: whether a
// session still answers for this caller, and whether this caller holds each grant a
// topic's policy names. Both are asked of every connection, including one whose
// topics name nobody's grant: the session question is the same question either way,
// and a topic a caller may watch for being signed in is still a topic a person who is
// nobody here may not watch.
//
// It is asked of the same Authority the heartbeat asks, so the connect decision and
// the re-check are one decision made twice rather than two implementations that can
// drift.
//
// The tenant's own boundary is answered first, and without asking: a permission the
// declaring module marked as the operator's is refused a tenant that is not the
// operator's before that tenant's authorizer is consulted, because its answer is
// about that tenant's roles and the question is not. This is httpx's order — the
// declaration, then the tenant, then the authorizer — and a stream that asked in a
// different order would let an operator permission reach a customer as soon as it was
// streamed.
//
// The transaction it opens is the connection's own, which is what the Detached is
// for. The request that arrived has already been answered as far as the guards are
// concerned: kit/httpx commits their transaction before the handler runs, and for a
// streamed response it does so precisely because a stream outlives a query. Handing
// that context to the Authority would leave it holding the request's own Pending —
// spent, still reachable, and able to open again — and the transaction it opened
// would then be ended by the request rather than by this call, which is a pool
// connection pinned idle in a transaction for the life of the stream.
func (h *Hub) admit(ctx context.Context, req Request, topics []Topic) error {
	asks, outside := askable(req.Tenant, topics)
	if len(outside) > 0 {
		return &Refusal{Status: http.StatusForbidden,
			Detail: httpx.CodeNotOperator + fmt.Sprintf(": the topic %s requires %s, which is the operator's and this is not the operator's tenant", named(topics, outside[0]), outside[0])}
	}
	renewal, err := h.authority.Renew(db.Detached(ctx), req.Tenant, req.HTTP, asks)
	if err != nil {
		return &Refusal{Status: http.StatusServiceUnavailable,
			Detail: CodeUnavailable + ": this application cannot check what this caller may watch right now"}
	}
	if !renewal.SignedIn {
		return &Refusal{Status: http.StatusForbidden,
			Detail: httpx.CodeAnonymous + ": no session answers for this caller"}
	}
	for _, t := range topics {
		if t.Policy.Kind() != module.KindGrant {
			continue
		}
		if slices.Contains(renewal.Refused, t.Policy.Grant()) {
			return &Refusal{Status: http.StatusForbidden,
				Detail: httpx.CodeDenied + fmt.Sprintf(": the topic %s requires %s, which this caller does not hold", t.Event, t.Policy.Grant())}
		}
	}
	return nil
}

// named is the first topic whose policy names this grant, for the refusal that has to
// say which topic it refused.
func named(topics []Topic, grant string) string {
	for _, t := range topics {
		if t.Policy.Kind() == module.KindGrant && t.Policy.Grant() == grant {
			return t.Event
		}
	}
	return ""
}

// register takes one connection's slots: this person's share of them in this
// tenant, and this tenant's share of the process. The count and the check are one
// critical section, so two connections racing for the last slot cannot both take it.
func (h *Hub) register(req Request, topics []Topic) (*connection, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	seat := seatKey{tenant: req.Tenant.ID, user: req.Principal.UserID}
	if h.perPrincipal[seat] >= h.limits.MaxPerPrincipal {
		return nil, &Refusal{Status: http.StatusTooManyRequests, RetryAfter: h.limits.RetryAfter,
			Detail: httpx.CodeLimitExhausted + fmt.Sprintf(": this caller already holds %d live connections in this tenant; close one before opening another", h.limits.MaxPerPrincipal)}
	}
	if h.perTenant[req.Tenant.ID] >= h.limits.MaxPerTenant {
		return nil, &Refusal{Status: http.StatusTooManyRequests, RetryAfter: h.limits.RetryAfter,
			Detail: httpx.CodeLimitExhausted + fmt.Sprintf(": this tenant already holds %d live connections in this process", h.limits.MaxPerTenant)}
	}
	h.perPrincipal[seat]++
	h.perTenant[req.Tenant.ID]++
	c := &connection{
		user:       req.Principal.UserID,
		tenant:     req.Tenant,
		request:    req.HTTP,
		topics:     make(map[string]Topic, len(topics)),
		notices:    make(chan Notice, h.limits.Queue),
		overflowed: make(chan struct{}, 1),
	}
	for _, t := range topics {
		c.topics[t.Event] = t
	}
	return c, nil
}

// watchTopic takes the shared observation of one (tenant, event), or joins the one
// another connection of this hub already holds — including one still being opened,
// which is the case that makes this a wait rather than a check. The mutex is never held across
// Observe: a broker round trip belongs to no critical section.
//
// The observation is opened on a context of the watch's own life rather than on the
// connection that happened to ask first. A transport ends an observation when the
// context it was handed ends, and a watch outlives whichever connection opened it:
// an observation bound to the first connection's context would go silent for every
// other connection of that (tenant, event) the moment that one hung up, while this
// hub still believed the watch was live. The cancel belongs to the watch, goes with
// its stop, and is what keeps the transport's own watcher on that context from
// outliving the observation.
//
// The mutex is never held across Observe, and what it does hold is the decision to
// dial. Per key, only one connection is ever told to dial: two observations of one
// address both left installed would each hand this hub the same event, and the hub
// would queue it twice — a duplicated notice is a client told to refetch twice over
// one thing that happened. Stopping the loser is not the answer: the provider holds
// the callback from the moment Observe returns, and a delivery already running is not
// recalled by a stop that has not been called yet. So the loser is never dialled.
func (h *Hub) watchTopic(ctx context.Context, c *connection, t Topic) error {
	key := watchKey{tenant: c.tenant.ID, event: t.Event}
	dial, err := h.reserveWatch(ctx, key, c)
	if err != nil || !dial {
		return err
	}
	return h.dialWatch(ctx, key, c)
}

// reserveWatch answers one connection's ask for one watch key: it takes the
// connection's place in a live watch and reports false, or reports true to the one
// connection that is to open it. A failed dial leaves no watch and no slot behind, so
// a connection that waited takes its own turn rather than inheriting a failure that
// was somebody else's round trip.
func (h *Hub) reserveWatch(ctx context.Context, key watchKey, c *connection) (dial bool, err error) {
	h.mu.Lock()
	w, live := h.watches[key]
	opening, dialling := h.dialling[key]
	if live {
		w.conns[c] = struct{}{}
		h.mu.Unlock()
		return false, nil
	}
	if dialling {
		// Waiting is the whole cost of opening an address once. Whoever dialled owns
		// the round trip; this connection asks again the moment it ends, and joins the
		// watch it installed — or takes its turn if there is nothing to join.
		h.mu.Unlock()
		select {
		case <-opening:
			return h.reserveWatch(ctx, key, c)
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	h.dialling[key] = make(chan struct{})
	h.mu.Unlock()
	return true, nil
}

// dialWatch opens one address through the transport and installs the watch that every
// connection waiting on it will join. The slot taken by reserveWatch is what makes
// this the only writer for the key, so the map holds one observation per address no
// matter how many connections arrived at it at once.
func (h *Hub) dialWatch(ctx context.Context, key watchKey, c *connection) error {
	life, cancel := context.WithCancel(context.WithoutCancel(ctx))
	stop, err := h.transport.Observe(life, key.tenant, key.event, func(ev transport.Event) {
		h.deliver(key, ev)
	})
	leave := func() {
		cancel()
		stop()
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if opening, ok := h.dialling[key]; ok {
		delete(h.dialling, key)
		close(opening)
	}
	if err != nil {
		// The context this watch would have lived on is the only thing it held, and it
		// goes with the failure: a transport that refused the consumer keeps no
		// observation, and nothing of this one is left running.
		cancel()
		return err
	}
	h.watches[key] = &watch{stop: leave, conns: map[*connection]struct{}{c: {}}}
	return nil
}

// deliver hands one event to every connection watching that address whose own
// policy lets it see this one. It never blocks and never fails: a connection whose
// queue is full is a connection that will be told it missed something, and a
// publisher is never delayed by a subscriber that stopped reading.
func (h *Hub) deliver(key watchKey, ev transport.Event) {
	// The connections and each one's own view of the topic are read together under
	// the mutex, because a withdrawal and a delivery are the same decision made from
	// two directions. The notices then go out outside it.
	type address struct {
		c     *connection
		topic Topic
	}
	h.mu.Lock()
	w, ok := h.watches[key]
	if !ok {
		h.mu.Unlock()
		return
	}
	targets := make([]address, 0, len(w.conns))
	for c := range w.conns {
		t, watching := c.topics[ev.Name]
		if !watching {
			// A connection that withdrew this topic while the watch was shared by
			// another of its own connections: its queue is no longer this one's
			// business.
			continue
		}
		targets = append(targets, address{c: c, topic: t})
	}
	h.mu.Unlock()

	notice := Notice{ID: ev.ID, Topic: ev.Name, At: ev.At}
	for _, target := range targets {
		if !h.maySee(target.c, target.topic, ev) {
			continue
		}
		select {
		case target.c.notices <- notice:
		default:
			// Overflow, once: the queue is dropped, so what reaches the connection
			// after this is the resync and then the live edge, and one frame says
			// both.
			if target.c.overflow.CompareAndSwap(false, true) {
				select {
				case target.c.overflowed <- struct{}{}:
				default:
				}
			}
			// Each receive is guarded by a default rather than by a length check,
			// because this runs on the goroutine of whoever published the event: between
			// "there is something in there" and "I took it" the connection's own writer
			// may have taken the last one, and a publisher waiting on a queue that has
			// just gone empty is a slow reader stalling the writer of the event.
		drain:
			for {
				select {
				case <-target.c.notices:
				default:
					break drain
				}
			}
		}
	}
}

// watching reports whether this connection still holds one topic. It is the question
// a notice has to pass on its way to the wire, and it is asked under the same mutex as
// the withdrawal that would take the topic away, so a frame for a grant the caller has
// lost cannot slip past the decision that withdrew it.
func (h *Hub) watching(c *connection, event string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := c.topics[event]
	return ok
}

// maySee applies a topic's per-notice filter. Only StreamRecipient has one: the
// server reads the named member of the payload, compares it to the connection's own
// principal, and lets no part of the payload go further than this function. A member
// that is not a user id is nobody's notice — dropped, and said once per event,
// because a payload that cannot answer who it is for reaches nobody.
func (h *Hub) maySee(c *connection, t Topic, ev transport.Event) bool {
	if t.Policy.Kind() != module.KindRecipient {
		return true
	}
	id, ok := namedUser(ev.Payload, t.Policy.Field())
	if !ok {
		h.log.ErrorContext(context.Background(), "stream: a recipient topic names no readable recipient",
			"event", ev.Name, "id", ev.ID, "field", t.Policy.Field())
		return false
	}
	return id == c.user
}

// namedUser reads one member of a payload as a user id. The payload is parsed as
// members rather than against a schema, because the schema of an event lives in
// kit/events' declared payload type and a second one here would be a second one to
// keep honest.
func namedUser(payload []byte, field string) (uuid.UUID, bool) {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(payload, &members); err != nil {
		return uuid.Nil, false
	}
	var id uuid.UUID
	if err := json.Unmarshal(members[field], &id); err != nil {
		return uuid.Nil, false
	}
	return id, id != uuid.Nil
}

// release gives back the connection's slots and every watch it held. The last holder
// of a watch stops it, outside the mutex: a withdrawal never waits on a delivery, and
// no broker round trip belongs in a critical section.
func (h *Hub) release(c *connection) {
	h.mu.Lock()
	seat := seatKey{tenant: c.tenant.ID, user: c.user}
	if h.perPrincipal[seat]--; h.perPrincipal[seat] <= 0 {
		delete(h.perPrincipal, seat)
	}
	if h.perTenant[c.tenant.ID]--; h.perTenant[c.tenant.ID] <= 0 {
		delete(h.perTenant, c.tenant.ID)
	}
	var stopped []func()
	for event := range c.topics {
		if stop, gone := h.dropWatch(watchKey{tenant: c.tenant.ID, event: event}, c); gone {
			stopped = append(stopped, stop)
		}
	}
	h.mu.Unlock()
	for _, stop := range stopped {
		stop()
	}
}

// dropWatch removes one connection from one watch and reports the stop function when
// it was the last holder. It runs under the hub's mutex.
func (h *Hub) dropWatch(key watchKey, c *connection) (func(), bool) {
	w, ok := h.watches[key]
	if !ok {
		return nil, false
	}
	delete(w.conns, c)
	if len(w.conns) > 0 {
		return nil, false
	}
	delete(h.watches, key)
	return w.stop, true
}

// withdraw drops one topic of a live connection: its frame goes out, its watch is
// released, and the connection itself ends when nothing is left to watch. What it does
// not do is drain the connection's queue — the notices already there belong to a
// writer that has not decided what to send, and loop drops them at the write.
func (h *Hub) withdraw(c *connection, t Topic, w http.ResponseWriter, rc *http.ResponseController) error {
	h.mu.Lock()
	delete(c.topics, t.Event)
	stop, _ := h.dropWatch(watchKey{tenant: c.tenant.ID, event: t.Event}, c)
	empty := len(c.topics) == 0
	h.mu.Unlock()
	if stop != nil {
		defer stop()
	}
	if err := h.write(w, rc, c, frame{event: "dropped", data: droppedData{Topic: t.Event, Reason: reasonPermission}}.bytes()); err != nil {
		return err
	}
	if empty {
		return errNoTopicsLeft
	}
	return nil
}

// askable splits one connection's topics into the permissions its tenant's authorizer
// may be asked about, and the permissions that tenant may not exercise at all. The
// second half is the declaration's operator boundary, and it is answered here rather
// than by the caller for the same reason the connect check asks the same Authority the
// heartbeat does: the two decisions are one decision, and a tenant's authorizer
// answering about a permission that is not the tenant's is a question it cannot answer
// honestly — the module's own authorizer reads its tenant's roles and reads them as
// its tenant's. One permission is one question however many topics name it.
func askable(tenant tenancy.Tenant, topics []Topic) (asks []string, outside []string) {
	for _, t := range topics {
		if t.Policy.Kind() != module.KindGrant {
			continue
		}
		grant := t.Policy.Grant()
		if slices.Contains(asks, grant) || slices.Contains(outside, grant) {
			continue
		}
		if t.Operator && !tenant.Operator {
			outside = append(outside, grant)
			continue
		}
		asks = append(asks, grant)
	}
	return asks, outside
}

// refused is the log line behind every refusal: who asked, what for, and the fact
// that answers it. A stream is not a state change, so this — and not an audit row —
// is the trail a connection leaves.
func (h *Hub) refused(ctx context.Context, req Request, topics []string, why string) {
	h.log.WarnContext(ctx, "stream: a live connection was refused",
		"tenant", req.Tenant.ID, "user", req.Principal.UserID, "topics", strings.Join(topics, ","), "why", why)
}
