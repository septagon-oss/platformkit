package stream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/events/transport"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// recorder is a response a test can read: every byte it was given, and the truth
// about whether it could be flushed or bounded. A case that needs a writer whose
// flush fails says so with flushErr rather than with a different type per case.
type recorder struct {
	header   http.Header
	body     strings.Builder
	mu       sync.Mutex
	flushErr error
	flushes  int
	// block makes every write wait on it, which is how a case makes a connection
	// that cannot keep up: the queue fills behind a write that is not coming back.
	block chan struct{}
}

// hold stops this writer in the middle of the next write and hands back the channel
// that lets it go.
func (r *recorder) hold() chan struct{} {
	gate := make(chan struct{})
	r.mu.Lock()
	r.block = gate
	r.mu.Unlock()
	return gate
}

func newRecorder() *recorder { return &recorder{header: http.Header{}} }

func (r *recorder) Header() http.Header { return r.header }

func (r *recorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	gate := r.block
	r.mu.Unlock()
	if gate != nil {
		<-gate
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.body.Write(p)
}

func (r *recorder) WriteHeader(int) {}

func (r *recorder) Flush() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flushes++
}

// FlushError is the form http.ResponseController asks for first, and the one this
// package's writes depend on: answering it is what lets a case put a failure under a
// stream and watch the stream find out.
func (r *recorder) FlushError() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flushes++
	return r.flushErr
}

func (r *recorder) written() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.body.String()
}

// flushed is the count of flushes, read under the same lock that writes it: a case
// waiting for a connection to open is reading it while the connection's own
// goroutine is the one incrementing it.
func (r *recorder) flushed() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.flushes
}

// plainWriter is a response with no flusher anywhere under it — the shape of a proxy
// that buffers whole answers in front of this process.
type plainWriter struct {
	header http.Header
	body   strings.Builder
}

func (p *plainWriter) Header() http.Header {
	if p.header == nil {
		p.header = http.Header{}
	}
	return p.header
}

func (p *plainWriter) Write(b []byte) (int, error) { return p.body.Write(b) }

func (p *plainWriter) WriteHeader(int) {}

// frameRead is one parsed frame: what a client's EventSource would see.
type frameRead struct {
	id    string
	event string
	data  map[string]any
}

func parse(t *testing.T, body string) []frameRead {
	t.Helper()
	var out []frameRead
	for _, chunk := range strings.Split(body, "\n\n") {
		if chunk == "" || strings.HasPrefix(chunk, ":") {
			continue
		}
		f := frameRead{data: map[string]any{}}
		for _, line := range strings.Split(chunk, "\n") {
			if strings.HasPrefix(line, ":") {
				continue // a comment reaches no handler in the browser
			}
			name, value, found := strings.Cut(line, ":")
			if !found {
				t.Fatalf("frame line %q carries no field", line)
			}
			switch name {
			case "id":
				f.id = strings.TrimSpace(value)
			case "event":
				f.event = strings.TrimSpace(value)
			case "data":
				if err := json.Unmarshal([]byte(strings.TrimSpace(value)), &f.data); err != nil {
					t.Fatalf("data %q is not one JSON object: %v", value, err)
				}
			case "retry":
			default:
				t.Fatalf("unexpected frame field %q in %q", name, chunk)
			}
		}
		if f.event == "" {
			continue
		}
		out = append(out, f)
	}
	return out
}

// fakeTransport is an events transport the test publishes through by hand, counting
// what it was asked to observe and how often it was stopped: the two numbers the
// shared-watch and reference-count cases are checked with.
type fakeTransport struct {
	mu       sync.Mutex
	observed map[string]int
	stopped  map[string]int
	deliver  map[string]func(transport.Event)
}

func newFakeTransport() *fakeTransport {
	return &fakeTransport{observed: map[string]int{}, stopped: map[string]int{}, deliver: map[string]func(transport.Event){}}
}

func (f *fakeTransport) Publish(context.Context, transport.Event) error { return nil }

func (f *fakeTransport) Subscribe(context.Context, string, string, transport.Sink) error { return nil }

func (f *fakeTransport) Observe(_ context.Context, tenant uuid.UUID, name string, fn func(transport.Event)) (func(), error) {
	key := tenant.String() + " " + name
	f.mu.Lock()
	f.observed[key]++
	f.deliver[key] = fn
	f.mu.Unlock()
	return func() {
		f.mu.Lock()
		f.stopped[key]++
		f.mu.Unlock()
	}, nil
}

func (f *fakeTransport) count(tenant uuid.UUID, name string) (observed, stopped int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := tenant.String() + " " + name
	return f.observed[key], f.stopped[key]
}

// send hands one event to whoever observes that tenant and name — the broker's
// delivery, which is the only way in: an observer of another tenant, or of another
// name, is not in the map.
func (f *fakeTransport) send(tenant uuid.UUID, ev transport.Event) {
	f.mu.Lock()
	fn := f.deliver[tenant.String()+" "+ev.Name]
	f.mu.Unlock()
	if fn != nil {
		fn(ev)
	}
}

// fakeAuthority is the composition's answer, scripted: a case says what the next
// question should hear, and both the connect check and the heartbeat obey it.
type fakeAuthority struct {
	mu      sync.Mutex
	renewal Renewal
	err     error
	calls   int
}

func (f *fakeAuthority) Renew(context.Context, tenancy.Tenant, *http.Request, []string) (Renewal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.renewal, f.err
}

func (f *fakeAuthority) answer(r Renewal) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.renewal = r
}

func (f *fakeAuthority) fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

type fixture struct {
	hub       *Hub
	transport *fakeTransport
	authority *fakeAuthority
	tenants   [2]tenancy.Tenant
	users     [2]uuid.UUID
}

func newFixture(t *testing.T, streams []module.Stream, limits Limits) *fixture {
	t.Helper()
	f := &fixture{transport: newFakeTransport(), authority: &fakeAuthority{renewal: Renewal{SignedIn: true}}}
	f.tenants = [2]tenancy.Tenant{{ID: uuid.New(), Slug: "alpha"}, {ID: uuid.New(), Slug: "beta"}}
	f.users = [2]uuid.UUID{uuid.New(), uuid.New()}
	f.hub = New(Deps{
		Transport: f.transport,
		Registry:  NewRegistry([]module.Module{{Name: "task", Events: []string{"task.closed", "task.opened", "task.private", "task.assigned"}, Streams: streams}}),
		Authority: f.authority,
		Limits:    limits,
		Clock:     fixedClock{at: time.Date(2026, 10, 14, 9, 40, 0, 0, time.UTC)},
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	return f
}

type fixedClock struct{ at time.Time }

func (f fixedClock) Now() time.Time { return f.at }

func (f *fixture) request(tenant, user int, topics ...string) Request {
	return Request{
		Tenant:    f.tenants[tenant],
		Principal: tenancy.Principal{UserID: f.users[user]},
		HTTP:      httptest.NewRequest(http.MethodGet, "/api/v1/app/stream?topics=x", nil),
		Topics:    topics,
	}
}

// slowHeartbeat is a connection that will not be re-checked while the case is still
// deciding what the re-check should answer with.
func slowHeartbeat() Limits {
	return Limits{MaxTopics: 8, MaxPerPrincipal: 3, MaxPerTenant: 500, Queue: 64,
		WriteDeadline: time.Second, Heartbeat: time.Hour, RetryAfter: 5 * time.Second}
}

// fastHeartbeat is a connection the case wants re-checked while it watches.
func fastHeartbeat() Limits {
	return Limits{MaxTopics: 8, MaxPerPrincipal: 3, MaxPerTenant: 500, Queue: 64,
		WriteDeadline: time.Second, Heartbeat: 10 * time.Millisecond, RetryAfter: 5 * time.Second}
}

type live struct {
	w    *recorder
	done chan error
	stop context.CancelFunc
}

// open starts one connection and waits for its opening frame, which is the moment
// the watches are held and the response is on the wire.
func (f *fixture) open(t *testing.T, req Request) *live {
	t.Helper()
	w := newRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- f.hub.Serve(ctx, w, req) }()
	if !until(func() bool { return w.flushed() > 0 }) {
		t.Fatalf("the connection never opened: %v", <-done)
	}
	return &live{w: w, done: done, stop: cancel}
}

func (l *live) close(t *testing.T) {
	t.Helper()
	l.stop()
	if err := <-l.done; err != nil {
		t.Errorf("Serve answered %v after the client hung up", err)
	}
}

func (l *live) await(t *testing.T, want string) {
	t.Helper()
	if !until(func() bool { return strings.Contains(l.w.written(), want) }) {
		t.Fatalf("no %q reached the connection; it wrote:\n%s", want, l.w.written())
	}
}

func until(ready func() bool) bool {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return false
}

func refuse(t *testing.T, err error) *Refusal {
	t.Helper()
	var refusal *Refusal
	if !errors.As(err, &refusal) {
		t.Fatalf("Serve answered %v; want a *stream.Refusal", err)
	}
	return refusal
}

func event(t *testing.T, name string, tenant uuid.UUID, payload string) transport.Event {
	t.Helper()
	return transport.Event{ID: uuid.New(), Name: name, TenantID: tenant,
		Payload: json.RawMessage(payload), At: time.Date(2026, 10, 14, 9, 41, 0, 0, time.UTC)}
}

func TestAPublishedNoticeNeverReachesAnotherTenantsConnection(t *testing.T) {
	f := newFixture(t, []module.Stream{{Event: "task.closed", Policy: module.StreamAnyPrincipal()}}, slowHeartbeat())
	closed := []module.Stream{{Event: "task.closed", Policy: module.StreamAnyPrincipal()}}
	_ = closed
	a := f.open(t, f.request(0, 0, "task.closed"))
	defer a.close(t)

	// Tenant B's event, delivered to the only watch of tenant B — which is nobody's
	// connection here. The transport routes by address, so this is the whole of the
	// isolation: tenant A's connection has no path to it.
	f.transport.send(f.tenants[1].ID, event(t, "task.closed", f.tenants[1].ID, `{}`))
	time.Sleep(20 * time.Millisecond)
	if strings.Contains(a.w.written(), "task.closed\n") {
		t.Errorf("tenant A's connection received a frame for tenant B's event:\n%s", a.w.written())
	}

	b := f.open(t, f.request(1, 1, "task.closed"))
	defer b.close(t)
	f.transport.send(f.tenants[1].ID, event(t, "task.closed", f.tenants[1].ID, `{}`))
	b.await(t, "event: task.closed")
	if got := strings.Count(b.w.written(), "event: task.closed"); got != 1 {
		t.Errorf("tenant B's own connection saw the publish %d times; want once", got)
	}
}

func TestTheOpeningFramesOfEveryConnectionCarryNoPositionToResumeFrom(t *testing.T) {
	f := newFixture(t, []module.Stream{{Event: "task.closed", Policy: module.StreamAnyPrincipal()}}, slowHeartbeat())
	c := f.open(t, f.request(0, 0, "task.closed"))
	defer c.close(t)

	got := parse(t, c.w.written())
	if len(got) != 1 || got[0].event != "resync" {
		t.Fatalf("a connection opened with %v; want one resync", got)
	}
	if got[0].id != "" {
		t.Errorf("the opening frame carried id %q, which the server would then have to honour", got[0].id)
	}
	if got[0].data["reason"] != "connect" {
		t.Errorf("resync reason = %v; want connect", got[0].data["reason"])
	}
	if !strings.HasPrefix(c.w.written(), "retry: 3000\n: pkit stream\n\n") {
		t.Errorf("the connection did not open with the retry and the comment: %q", c.w.written())
	}
	if got := c.w.header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q; the App surface refuses a storable value under a session", got)
	}
	if got := c.w.header.Get("X-Accel-Buffering"); got != "no" {
		t.Errorf("X-Accel-Buffering = %q", got)
	}
}

func TestANoticeCarriesItsEventItsIdAndItsTimeAndNoPayload(t *testing.T) {
	f := newFixture(t, []module.Stream{{Event: "task.closed", Policy: module.StreamAnyPrincipal()}}, slowHeartbeat())
	c := f.open(t, f.request(0, 0, "task.closed"))
	defer c.close(t)

	ev := event(t, "task.closed", f.tenants[0].ID, `{"body":"the payload never leaves the server"}`)
	f.transport.send(f.tenants[0].ID, ev)
	c.await(t, "event: task.closed")

	got := parse(t, c.w.written())
	notice := got[len(got)-1]
	if notice.id != ev.ID.String() {
		t.Errorf("notice id = %q; want the event's %s", notice.id, ev.ID)
	}
	if notice.data["time"] != "2026-10-14T09:41:00Z" {
		t.Errorf("notice time = %v; want the instant the state changed, in UTC", notice.data["time"])
	}
	if len(notice.data) != 1 {
		t.Errorf("a notice carried %v; its only member is time", notice.data)
	}
	if strings.Contains(c.w.written(), "payload") {
		t.Error("a frame carried a payload")
	}
}

func TestAnUndeclaredTopicRefusesTheWholeConnection(t *testing.T) {
	f := newFixture(t, []module.Stream{{Event: "task.closed", Policy: module.StreamAnyPrincipal()}}, slowHeartbeat())
	w := newRecorder()
	err := f.hub.Serve(context.Background(), w, f.request(0, 0, "task.closed", "task.somethingelse"))
	refusal := refuse(t, err)
	if refusal.Status != http.StatusForbidden {
		t.Errorf("status = %d; want 403", refusal.Status)
	}
	if !strings.Contains(refusal.Detail, "task.somethingelse") {
		t.Errorf("refusal %q does not name the topic", refusal.Detail)
	}
	if w.written() != "" {
		t.Errorf("a refusal wrote %q before it refused", w.written())
	}
	if got := w.header.Get("Content-Type"); got != "" {
		t.Errorf("a refusal set Content-Type %q; the stream's headers belong to a stream", got)
	}
}

func TestAPermissionTopicRefusesSomeoneWithoutTheGrantAndStreamsItsHolder(t *testing.T) {
	f := newFixture(t, []module.Stream{
		{Event: "task.private", Policy: module.StreamPermission("task:manage")},
	}, slowHeartbeat())

	w := newRecorder()
	f.authority.answer(Renewal{SignedIn: true, Refused: []string{"task:manage"}})
	err := f.hub.Serve(context.Background(), w, f.request(0, 0, "task.private"))
	refusal := refuse(t, err)
	if refusal.Status != http.StatusForbidden {
		t.Fatalf("status = %d; want 403", refusal.Status)
	}
	if !strings.Contains(refusal.Detail, "task.private") || !strings.Contains(refusal.Detail, "task:manage") {
		t.Errorf("refusal %q names neither the topic nor the grant", refusal.Detail)
	}
	if w.written() != "" {
		t.Errorf("a refusal wrote %q", w.written())
	}

	f.authority.answer(Renewal{SignedIn: true})
	c := f.open(t, f.request(0, 0, "task.private"))
	defer c.close(t)
	c.await(t, "event: resync")
}

func TestARecipientTopicReachesItsRecipientAndNobodyElse(t *testing.T) {
	f := newFixture(t, []module.Stream{{Event: "task.assigned", Policy: module.StreamRecipient("assignee")}}, slowHeartbeat())
	other := f.open(t, f.request(0, 0, "task.assigned"))
	defer other.close(t)
	recipient := f.open(t, f.request(0, 1, "task.assigned"))
	defer recipient.close(t)

	f.transport.send(f.tenants[0].ID, event(t, "task.assigned", f.tenants[0].ID,
		fmt.Sprintf(`{"assignee":%q}`, f.users[1])))
	recipient.await(t, "event: task.assigned")
	if strings.Contains(other.w.written(), "event: task.assigned") {
		t.Error("a notice addressed to one person reached another's connection")
	}

	// A payload whose named member is not a user id is nobody's notice: dropped, and
	// said in the log rather than delivered to everyone as a guess.
	f.transport.send(f.tenants[0].ID, event(t, "task.assigned", f.tenants[0].ID, `{"assignee":"nobody"}`))
	time.Sleep(20 * time.Millisecond)
	if got := strings.Count(recipient.w.written(), "event: task.assigned"); got != 1 {
		t.Errorf("an unreadable recipient delivered anyway: %d notices", got)
	}
}

func TestAConnectionWatchesEightTopicsAndANinthIsRefused(t *testing.T) {
	var streams []module.Stream
	var topics []string
	for i := range 9 {
		name := fmt.Sprintf("task.event%d", i)
		streams = append(streams, module.Stream{Event: name, Policy: module.StreamAnyPrincipal()})
		topics = append(topics, name)
	}
	f := newFixture(t, streams, slowHeartbeat())

	w := newRecorder()
	err := f.hub.Serve(context.Background(), w, f.request(0, 0, strings.Join(topics, ",")))
	refusal := refuse(t, err)
	if refusal.Status != http.StatusBadRequest {
		t.Fatalf("nine topics answered status %d; want 400", refusal.Status)
	}
	if !strings.Contains(refusal.Detail, "8") || !strings.Contains(refusal.Detail, "9") {
		t.Errorf("refusal %q names neither the limit nor the count", refusal.Detail)
	}

	c := f.open(t, f.request(0, 0, strings.Join(topics[:8], ",")))
	defer c.close(t)
	if observed, _ := f.transport.count(f.tenants[0].ID, "task.event7"); observed != 1 {
		t.Errorf("the eighth topic was observed %d times", observed)
	}
}

func TestACommaSeparatedListAndARepeatedOneMeanOneConnection(t *testing.T) {
	f := newFixture(t, []module.Stream{
		{Event: "task.closed", Policy: module.StreamAnyPrincipal()},
		{Event: "task.opened", Policy: module.StreamAnyPrincipal()},
	}, slowHeartbeat())
	c := f.open(t, f.request(0, 0, "task.closed, task.opened,", "task.closed"))
	defer c.close(t)
	for _, name := range []string{"task.closed", "task.opened"} {
		if observed, _ := f.transport.count(f.tenants[0].ID, name); observed != 1 {
			t.Errorf("%s was observed %d times; the normalisation is meant to see each topic once", name, observed)
		}
	}

	w := newRecorder()
	if refusal := refuse(t, f.hub.Serve(context.Background(), w, f.request(0, 0, "task.closed,Not An Event"))); !strings.Contains(refusal.Detail, "Not An Event") {
		t.Errorf("an ill-formed topic answered %q; it names what the caller got wrong", refusal.Detail)
	}
}

func TestAFourthConnectionFromOnePersonAnswers429WithTheWaitItNamed(t *testing.T) {
	f := newFixture(t, []module.Stream{{Event: "task.closed", Policy: module.StreamAnyPrincipal()}}, slowHeartbeat())
	var held []*live
	for range 3 {
		held = append(held, f.open(t, f.request(0, 0, "task.closed")))
	}
	w := newRecorder()
	refusal := refuse(t, f.hub.Serve(context.Background(), w, f.request(0, 0, "task.closed")))
	if refusal.Status != http.StatusTooManyRequests {
		t.Errorf("status = %d; want 429", refusal.Status)
	}
	if refusal.RetryAfter != 5*time.Second {
		t.Errorf("the 429 named %s; want the wait the limits named", refusal.RetryAfter)
	}
	if w.written() != "" {
		t.Errorf("a refusal wrote %q", w.written())
	}
	// Another person is not at their ceiling because of this one's tabs, and the
	// three connections already open are still streaming.
	other := f.open(t, f.request(0, 1, "task.closed"))
	for _, h := range held {
		if !strings.Contains(h.w.written(), "resync") {
			t.Error("a held connection is no longer streaming")
		}
	}
	for _, h := range held {
		h.close(t)
	}
	other.close(t)
}

func TestATenantAtItsConnectionCeilingAnswers429(t *testing.T) {
	limits := slowHeartbeat()
	limits.MaxPerTenant = 2
	f := newFixture(t, []module.Stream{{Event: "task.closed", Policy: module.StreamAnyPrincipal()}}, limits)
	var held []*live
	for range 2 {
		held = append(held, f.open(t, f.request(0, 0, "task.closed")))
	}
	refusal := refuse(t, f.hub.Serve(context.Background(), newRecorder(), f.request(0, 1, "task.closed")))
	if refusal.Status != http.StatusTooManyRequests {
		t.Errorf("the third connection of a tenant capped at two answered status %d", refusal.Status)
	}
	for _, h := range held {
		h.close(t)
	}
	// The ceiling is the tenant's own: the other tenant's connections are unaffected.
	other := f.open(t, f.request(1, 0, "task.closed"))
	other.close(t)
}

func TestOneWatchIsSharedByEveryConnectionToIt(t *testing.T) {
	f := newFixture(t, []module.Stream{{Event: "task.closed", Policy: module.StreamAnyPrincipal()}}, slowHeartbeat())
	var held []*live
	for range 3 {
		held = append(held, f.open(t, f.request(0, 0, "task.closed")))
	}
	if observed, _ := f.transport.count(f.tenants[0].ID, "task.closed"); observed != 1 {
		t.Fatalf("three connections on one (tenant, event) asked the transport to observe it %d times", observed)
	}
	f.transport.send(f.tenants[0].ID, event(t, "task.closed", f.tenants[0].ID, `{}`))
	for i, h := range held {
		if !strings.Contains(h.w.written(), "event: task.closed") {
			if !until(func() bool { return strings.Contains(h.w.written(), "event: task.closed") }) {
				t.Errorf("connection %d received no notice", i)
			}
		}
	}
	for _, h := range held {
		h.close(t)
	}
	if !until(func() bool { _, stopped := f.transport.count(f.tenants[0].ID, "task.closed"); return stopped == 1 }) {
		_, stopped := f.transport.count(f.tenants[0].ID, "task.closed")
		t.Errorf("the last connection left and the watch was stopped %d times; want once", stopped)
	}
}

func TestAnOverflowedQueueSendsExactlyOneResync(t *testing.T) {
	limits := slowHeartbeat()
	limits.Queue = 4
	f := newFixture(t, []module.Stream{{Event: "task.closed", Policy: module.StreamAnyPrincipal()}}, limits)
	c := f.open(t, f.request(0, 0, "task.closed"))
	// The connection stops keeping up: one write is in flight and not coming back,
	// so the queue behind it is the whole of what this connection may hold.
	gate := c.w.hold()
	for i := range 200 {
		f.transport.send(f.tenants[0].ID, event(t, "task.closed", f.tenants[0].ID, fmt.Sprintf(`{"n":%d}`, i)))
	}
	close(gate)
	if !until(func() bool { return strings.Contains(c.w.written(), `"reason":"overflow"`) }) {
		t.Fatalf("no overflow frame reached the connection:\n%s", c.w.written())
	}
	if got := strings.Count(c.w.written(), `"reason":"overflow"`); got != 1 {
		t.Errorf("200 notices against a blocked writer produced %d overflow frames; want exactly one", got)
	}
	c.stop()
	<-c.done
}

func TestAHeartbeatDropsARevokedTopicAndKeepsTheConnection(t *testing.T) {
	f := newFixture(t, []module.Stream{
		{Event: "task.closed", Policy: module.StreamPermission("task:read")},
		{Event: "task.opened", Policy: module.StreamAnyPrincipal()},
	}, fastHeartbeat())

	c := f.open(t, f.request(0, 0, "task.closed", "task.opened"))
	defer c.close(t)
	f.authority.answer(Renewal{SignedIn: true, Refused: []string{"task:read"}})

	c.await(t, `"topic":"task.closed"`)
	body := c.w.written()
	if !strings.Contains(body, `"reason":"permission"`) {
		t.Errorf("a revoked grant produced no dropped frame naming the reason: %s", body)
	}
	if !until(func() bool {
		_, stopped := f.transport.count(f.tenants[0].ID, "task.closed")
		return stopped == 1
	}) {
		_, stopped := f.transport.count(f.tenants[0].ID, "task.closed")
		t.Errorf("the withdrawn topic left its watch running (stopped %d times)", stopped)
	}
	// The connection itself stays: one topic left, and it still gets its notices.
	f.transport.send(f.tenants[0].ID, event(t, "task.opened", f.tenants[0].ID, `{}`))
	c.await(t, "event: task.opened")
	c.await(t, ": ping")
}

func TestAHeartbeatClosesAnEndedSession(t *testing.T) {
	f := newFixture(t, []module.Stream{{Event: "task.closed", Policy: module.StreamPermission("task:read")}}, fastHeartbeat())
	c := f.open(t, f.request(0, 0, "task.closed"))
	defer c.stop()
	c.await(t, ": ping")
	f.authority.answer(Renewal{SignedIn: false})
	select {
	case err := <-c.done:
		if err != nil {
			t.Errorf("Serve answered %v; an ended session closes quietly", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("an ended session left the connection open")
	}
	if !strings.Contains(c.w.written(), ": closing") {
		t.Error("the connection ended without saying so")
	}
	if !until(func() bool { _, stopped := f.transport.count(f.tenants[0].ID, "task.closed"); return stopped == 1 }) {
		t.Error("an ended session left its watch running")
	}
}

func TestAConnectionEmptiedOfTopicsCloses(t *testing.T) {
	f := newFixture(t, []module.Stream{{Event: "task.closed", Policy: module.StreamPermission("task:read")}}, fastHeartbeat())
	c := f.open(t, f.request(0, 0, "task.closed"))
	defer c.stop()
	c.await(t, ": ping")
	f.authority.answer(Renewal{SignedIn: true, Refused: []string{"task:read"}})
	select {
	case err := <-c.done:
		if err != nil {
			t.Errorf("Serve answered %v; a connection with nothing left to watch closes quietly", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a connection whose only topic was withdrawn stayed open")
	}
}

func TestAFailedHeartbeatRecheckLeavesTheConnectionUp(t *testing.T) {
	f := newFixture(t, []module.Stream{{Event: "task.closed", Policy: module.StreamPermission("task:read")}}, fastHeartbeat())
	c := f.open(t, f.request(0, 0, "task.closed"))
	defer c.close(t)
	c.await(t, ": ping")
	f.authority.fail(errors.New("the database is not answering"))

	// The outage is noticed and the connection stays, and a notice still goes: a
	// re-check that failed is not a refusal, and a four-second outage is not a
	// reason to drop every live connection in the tenant.
	f.transport.send(f.tenants[0].ID, event(t, "task.closed", f.tenants[0].ID, `{}`))
	c.await(t, "event: task.closed")
}

func TestAResponseThatCannotFlushIsRefusedBeforeAnyHeader(t *testing.T) {
	f := newFixture(t, []module.Stream{{Event: "task.closed", Policy: module.StreamAnyPrincipal()}}, slowHeartbeat())
	w := &plainWriter{}
	err := f.hub.Serve(context.Background(), w, f.request(0, 0, "task.closed"))
	refusal := refuse(t, err)
	if refusal.Status != http.StatusInternalServerError {
		t.Fatalf("Serve over a writer that cannot flush answered status %d", refusal.Status)
	}
	if w.body.Len() != 0 {
		t.Errorf("a stream began over a writer that cannot flush: %q", w.body.String())
	}
	if got := w.header.Get("Content-Type"); got != "" {
		t.Errorf("a refusal set Content-Type %q", got)
	}
}

func TestEveryFrameEndsInABlankLineAndCarriesOneSpace(t *testing.T) {
	f := newFixture(t, []module.Stream{{Event: "task.closed", Policy: module.StreamAnyPrincipal()}}, slowHeartbeat())
	c := f.open(t, f.request(0, 0, "task.closed"))
	defer c.close(t)
	f.transport.send(f.tenants[0].ID, event(t, "task.closed", f.tenants[0].ID, `{}`))
	c.await(t, "event: task.closed")

	body := c.w.written()
	if !strings.HasSuffix(body, "\n\n") {
		t.Errorf("the stream does not end in a blank line: %q", body[len(body)-8:])
	}
	for _, line := range strings.Split(body, "\n") {
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		name, _, _ := strings.Cut(line, ":")
		if !strings.HasPrefix(line, name+": ") {
			t.Errorf("frame field %q has no single space after its colon", line)
		}
	}
}
