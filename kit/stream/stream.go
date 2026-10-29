// Package stream sends event notices without payloads or replay cursors.
// Transport.Observe supplies the tenant boundary. Missed notices require resync.
// Each Hub owns its watches and counters; connection ceilings are process-local.
package stream

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events/transport"
	"github.com/septagon-oss/platformkit/kit/httpx"
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

// Authority rechecks sessions and distinct topic grants at admission and each
// heartbeat, even when asks is empty. Refused uses the same permission spellings.
// Implementations own a short transaction on the detached ctx. Operator scope is
// checked by Hub first. Renewal errors keep an admitted stream open until its
// next heartbeat; admission errors refuse opening it.
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
