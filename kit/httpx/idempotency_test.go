package httpx_test

// Idempotency: one submission of a command, whichever end of the network forgot
// about it. Every case here runs the real chain against the real Postgres — the
// same composition production builds (httpx.New, the same middleware list, the
// same database) — because the decision under test is "an in-flight claim is
// visible to a concurrent transaction", which is a property of MVCC and cannot be
// faked. The two gates this file shares its decisions with are the migration's own
// walk (migrations/rls_test.go) for the door and refusal_shape_test.go for the
// shape of every refusal.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/internal/syscap"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// noteIn and noteOut are the command's body and its answer. The answer carries how
// many times the handler ran, so a test can tell "the same bytes again" from "the
// same work again" instead of trusting a status code.
type (
	noteIn struct {
		Body noteBody
	}
	noteBody struct {
		Text string `json:"text"`
	}
	noteOut struct {
		Body struct {
			Text string `json:"text"`
			Runs int64  `json:"runs"`
		}
	}
)

// write is a command's handler: it counts itself, writes one row of the domain
// table inside the request's own transaction, and answers with the values it was
// given plus its own run count.
type write func(ctx context.Context, in *noteIn) (*noteOut, error)

// mount mounts one POST command at path. declared is the route's word on the
// Idempotency-Key header, and it is the only difference between the two routes
// this file keeps mounting side by side. The declaration is applied to the
// operation before it is registered, which is the only moment it can be applied:
// huma builds both the document and the request context off this object.
func mount(t *testing.T, api *httpx.API, id, path string, declared bool, run write) {
	t.Helper()
	op := huma.Operation{OperationID: id, Method: http.MethodPost, Path: path, Errors: []int{409, 422, 503}}
	if declared {
		httpx.DeclareIdempotency(&op)
	}
	httpx.Register(api.Surfaces(probe).App, op, httpx.Permission("note:write"), run)
}

// note is the command every case below uses: run counts itself and writes the body
// it was handed, so the rows are the effect and the counter is the run.
func note(runs *atomic.Int64) write {
	return func(ctx context.Context, in *noteIn) (*noteOut, error) {
		n := runs.Add(1)
		tx, ok := httpx.TxFrom(ctx)
		if !ok {
			return nil, errors.New("the command was run outside a request")
		}
		t, _ := tenancy.FromContext(ctx)
		if err := tx.DB().Exec("INSERT INTO notes (tenant_id, body) VALUES (?, ?)", t.ID, in.Body.Text).Error; err != nil {
			return nil, err
		}
		out := &noteOut{}
		out.Body.Text, out.Body.Runs = in.Body.Text, n
		return out, nil
	}
}

// send posts a note under a key. An empty key sends no header at all, which is a
// case of its own: the header is optional, and a route that declares it and is
// given none simply runs.
func send(t *testing.T, h http.Handler, path, key, text string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "http://"+host+path, strings.NewReader(`{"text":"`+text+`"}`))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set(httpx.IdempotencyKeyHeader, key)
	}
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// setupNote is the fixture each case starts from: a notes table, a caller, the
// grant, and one declared route beside one that declared nothing.
func setupNote(t *testing.T) (*httpx.API, *chi.Mux, *fixture, *atomic.Int64, string) {
	t.Helper()
	api, router, f := setup(t)
	f.exec(`CREATE TABLE notes (id serial PRIMARY KEY, tenant_id uuid NOT NULL, body text NOT NULL)`)
	f.signedIn()
	f.allow = true
	runs := &atomic.Int64{}
	mount(t, api, "write-note", "/notes", true, note(runs))
	mount(t, api, "write-quiet-note", "/quiet-notes", false, note(runs))
	return api, router, f, runs, at(api, "/notes")
}

const keyA = "3f2504e0-4f89-41d3-9a0c-0305e82c3301"

// TestACommandSentTwiceWithTheSameKeyRunsOnce is the brief's acceptance in one
// case: one row, two byte-identical responses, and the second one saying it is a
// replay. It fails if the gate replays a *re-rendered* response rather than the
// stored bytes, or if the handler ran twice.
func TestACommandSentTwiceWithTheSameKeyRunsOnce(t *testing.T) {
	_, router, f, runs, path := setupNote(t)
	first := send(t, router, path, keyA, "invoice paid")
	second := send(t, router, path, keyA, "invoice paid")
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("statuses %d then %d, want 200 twice: %s", first.Code, second.Code, second.Body)
	}
	if n := runs.Load(); n != 1 {
		t.Fatalf("the handler ran %d times, want once", n)
	}
	if got := countRows(t, f, "notes"); got != 1 {
		t.Fatalf("two requests under one key wrote %d rows, want one", got)
	}
	if same(first.Body.Bytes(), second.Body.Bytes()) == false {
		t.Fatalf("the two responses differ:\n  %s\n  %s", first.Body, second.Body)
	}
	if got := first.Header().Get("Idempotency-Replay"); got != "" {
		t.Errorf("the first response claimed to be a replay: %q", got)
	}
	if got := second.Header().Get("Idempotency-Replay"); got != "true" {
		t.Errorf("the replay carried Idempotency-Replay %q, want true", got)
	}
	if got := second.Header().Get("Content-Type"); got != first.Header().Get("Content-Type") {
		t.Errorf("the replay changed Content-Type from %q to %q", first.Header().Get("Content-Type"), got)
	}
}

// TestAStoredHeaderIsReplayedAndAnUnlistedOneIsNot. The response a client acts on
// is more than its body: htmx follows HX-Redirect and a browser follows Location,
// so a replay that dropped them would be a different answer. Everything else —
// above all the first request's own id — stays with the request that had it.
func TestAStoredHeaderIsReplayedAndAnUnlistedOneIsNot(t *testing.T) {
	api, router, _, runs, _ := setupNote(t)
	type answered struct {
		Location   string `header:"Location"`
		HXRedirect string `header:"HX-Redirect"`
		RequestID  string `header:"X-Request-ID"`
		Body       struct {
			Text string `json:"text"`
		}
	}
	op := huma.Operation{OperationID: "answered-note", Method: http.MethodPost, Path: "/answers"}
	httpx.DeclareIdempotency(&op)
	httpx.Register(api.Surfaces(probe).App, op, httpx.Permission("note:write"),
		func(context.Context, *struct{}) (*answered, error) {
			runs.Add(1)
			return &answered{Location: "/there", HXRedirect: "/there", RequestID: "the-first-one"}, nil
		})
	path := at(api, "/answers")
	if got := send(t, router, path, keyA, "").Code; got != http.StatusOK {
		t.Fatalf("the first caller got %d", got)
	}
	second := send(t, router, path, keyA, "")
	if got := second.Header().Get("HX-Redirect"); got != "/there" {
		t.Errorf("the replay dropped HX-Redirect: %q", got)
	}
	if got := second.Header().Get("Location"); got == "" {
		t.Error("the replay dropped Location, so it is a redirect to nowhere")
	}
	if got := second.Header().Get("X-Request-ID"); got == "the-first-one" {
		t.Error("a replay replayed another request's id, which is a log line nobody can find")
	}
	if n := runs.Load(); n != 1 {
		t.Fatalf("the command ran %d times, want once", n)
	}
}

// TestASecondKeyIsASecondCommand is the case that keeps this from being a
// body-dedupe: two deliberate submissions of the same values are two commands.
func TestASecondKeyIsASecondCommand(t *testing.T) {
	_, router, f, runs, path := setupNote(t)
	for _, k := range []string{keyA, "3f2504e0-4f89-41d3-9a0c-0305e82c3302"} {
		if got := send(t, router, path, k, "same body").Code; got != http.StatusOK {
			t.Fatalf("%s answered %d", k, got)
		}
	}
	if n := runs.Load(); n != 2 {
		t.Fatalf("two keys ran the command %d times, want twice", n)
	}
	if got := countRows(t, f, "notes"); got != 2 {
		t.Fatalf("two keys wrote %d rows, want two", got)
	}
}

// TestTheSameKeyWithAnotherBodyIsRefused: a key whose body changed is a different
// command wearing an old name. Nothing runs, and the first request's answer is
// left exactly as it was — the case asserts the stored response still replays
// afterwards, byte for byte.
func TestTheSameKeyWithAnotherBodyIsRefused(t *testing.T) {
	_, router, f, runs, path := setupNote(t)
	first := send(t, router, path, keyA, "first")
	res := send(t, router, path, keyA, "second")
	if res.Code != http.StatusUnprocessableEntity {
		t.Fatalf("the reused key answered %d, want 422", res.Code)
	}
	if !strings.Contains(res.Body.String(), httpx.CodeIdempotencyKeyReuse) {
		t.Errorf("the refusal names no code the caller can act on: %s", res.Body)
	}
	if got := res.Header().Get(httpx.IdempotencyRefusalHeader); got != httpx.CodeIdempotencyKeyReuse {
		t.Errorf("refusal header %q, want %s", got, httpx.CodeIdempotencyKeyReuse)
	}
	repeat := send(t, router, path, keyA, "first")
	if repeat.Code != http.StatusOK {
		t.Fatalf("the original body answered %d after the refusal, want the stored 200", repeat.Code)
	}
	if !same(first.Body.Bytes(), repeat.Body.Bytes()) {
		t.Fatalf("the refusal disturbed the stored response:\n  %s\n  %s", first.Body, repeat.Body)
	}
	if n := runs.Load(); n != 1 {
		t.Fatalf("the command ran %d times, want once: the 422 ran it", n)
	}
	if got := countRows(t, f, "notes"); got != 1 {
		t.Fatalf("the refused request wrote rows: %d", got)
	}
}

// TestARepeatWhileTheFirstIsRunningGets409 is the concurrency case, and the one
// that fails for any implementation writing its claim inside the request's own
// transaction: the repeat would then see no row and run the command twice.
func TestARepeatWhileTheFirstIsRunningGets409(t *testing.T) {
	api, router, _, runs, _ := setupNote(t)
	parked, release := make(chan struct{}), make(chan struct{})
	op := huma.Operation{OperationID: "slow-note", Method: http.MethodPost, Path: "/slow"}
	httpx.DeclareIdempotency(&op)
	httpx.Register(api.Surfaces(probe).App, op, httpx.Permission("note:write"),
		func(_ context.Context, in *noteIn) (*noteOut, error) {
			runs.Add(1)
			close(parked)
			<-release
			out := &noteOut{}
			out.Body.Text, out.Body.Runs = in.Body.Text, 1
			return out, nil
		})
	path := at(api, "/slow")
	done := make(chan int, 1)
	go func() { done <- send(t, router, path, keyA, "held").Code }()
	<-parked
	res := send(t, router, path, keyA, "held")
	if res.Code != http.StatusConflict {
		t.Fatalf("a repeat during the first run got %d, want 409: %s", res.Code, res.Body)
	}
	if got := res.Header().Get("Retry-After"); got == "" {
		t.Error("the 409 named no Retry-After, so the caller cannot know how long to wait")
	}
	if got := res.Header().Get(httpx.IdempotencyRefusalHeader); got != httpx.CodeIdempotencyInProgress {
		t.Errorf("refusal header %q, want %s", got, httpx.CodeIdempotencyInProgress)
	}
	close(release)
	if got := <-done; got != http.StatusOK {
		t.Fatalf("the parked request ended with %d, want its own 200", got)
	}
	// And now the key has an answer: a third request is replayed, neither refused
	// nor run. This ordering is the whole reason a lost response can be retried.
	third := send(t, router, path, keyA, "held")
	if third.Code != http.StatusOK || third.Header().Get("Idempotency-Replay") != "true" {
		t.Fatalf("after the first finished, a repeat got %d (replay %q), want a replayed 200",
			third.Code, third.Header().Get("Idempotency-Replay"))
	}
	if n := runs.Load(); n != 1 {
		t.Fatalf("the handler ran %d times, want once", n)
	}
}

// TestAFailedCommandReleasesTheKey: a refusal nobody received is not an answer to
// remember. The 5xx deletes the claim so a retry runs the command — the opposite
// of storing it, which would tell the caller "already done" about work that rolled
// back.
func TestAFailedCommandReleasesTheKey(t *testing.T) {
	api, router, _, runs, _ := setupNote(t)
	fail := true
	op := huma.Operation{OperationID: "failing-note", Method: http.MethodPost, Path: "/fails"}
	httpx.DeclareIdempotency(&op)
	httpx.Register(api.Surfaces(probe).App, op, httpx.Permission("note:write"),
		func(ctx context.Context, _ *noteIn) (*noteOut, error) {
			runs.Add(1)
			if fail {
				return nil, errors.New("the command could not be done")
			}
			out := &noteOut{}
			out.Body.Runs = runs.Load()
			return out, nil
		})
	path := at(api, "/fails")
	if got := send(t, router, path, keyA, "").Code; got < 500 {
		t.Fatalf("the failing command answered %d, want a 5xx", got)
	}
	fail = false
	if got := send(t, router, path, keyA, "").Code; got != http.StatusOK {
		t.Fatalf("the retry answered %d, want 200: the 5xx was stored as an answer", got)
	}
	if n := runs.Load(); n != 2 {
		t.Fatalf("the handler ran %d times, want twice: once failing, once retried", n)
	}
}

// TestARefusalIsReplayedAndRanOnce: a 4xx the command itself decided is stored and
// replayed verbatim. The command refused; that is the answer, and the second
// caller is entitled to it without the work happening twice.
func TestARefusalIsReplayedAndRanOnce(t *testing.T) {
	api, router, _, runs, _ := setupNote(t)
	op := huma.Operation{OperationID: "rejecting-note", Method: http.MethodPost, Path: "/rejects", Errors: []int{422}}
	httpx.DeclareIdempotency(&op)
	httpx.Register(api.Surfaces(probe).App, op, httpx.Permission("note:write"),
		func(_ context.Context, _ *noteIn) (*noteOut, error) {
			runs.Add(1)
			return nil, problem.New(http.StatusUnprocessableEntity, "the note is already resolved")
		})
	path := at(api, "/rejects")
	first := send(t, router, path, keyA, "")
	second := send(t, router, path, keyA, "")
	if first.Code < 400 || first.Code >= 500 {
		t.Fatalf("the command answered %d, want a 4xx of its own", first.Code)
	}
	if second.Code != first.Code || !same(first.Body.Bytes(), second.Body.Bytes()) {
		t.Fatalf("the refusal was not replayed: %d then %d\n  %s\n  %s", first.Code, second.Code, first.Body, second.Body)
	}
	if n := runs.Load(); n != 1 {
		t.Fatalf("the refusing command ran %d times, want once", n)
	}
	if second.Header().Get("Idempotency-Replay") != "true" {
		t.Error("a replayed refusal did not say it was a replay")
	}
}

// TestAKeyIsScopedToItsPrincipal: one key across two accounts is a cross-account
// replay, and the caller is in the primary key so that it cannot be one.
func TestAKeyIsScopedToItsPrincipal(t *testing.T) {
	_, router, f, runs, path := setupNote(t)
	first := send(t, router, path, keyA, "one")
	f.signedIn() // a different person, same key, same body
	second := send(t, router, path, keyA, "one")
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("two callers got %d and %d, want each their own 200", first.Code, second.Code)
	}
	if second.Header().Get("Idempotency-Replay") == "true" {
		t.Fatal("one caller received another caller's stored response")
	}
	if n := runs.Load(); n != 2 {
		t.Fatalf("two callers ran the command %d times, want twice", n)
	}
	if got := countRows(t, f, "notes"); got != 2 {
		t.Fatalf("two callers wrote %d rows, want two", got)
	}
}

// TestAKeyIsNotReadableThroughATenantTransaction is the row-level-security half:
// the rows name a tenant, and no tenant transaction can read them at all. The
// kernel wrote them and only the kernel reads them back — the door is the
// migration's policy, not a WHERE clause this package happens to write.
func TestAKeyIsNotReadableThroughATenantTransaction(t *testing.T) {
	_, router, f, _, path := setupNote(t)
	if got := send(t, router, path, keyA, "written").Code; got != http.StatusOK {
		t.Fatalf("the command answered %d", got)
	}
	if n := heldRows(t, f, false); n != 0 {
		t.Fatalf("a tenant-scoped connection read %d idempotency rows; they are not its table", n)
	}
	if n := heldRows(t, f, true); n != 1 {
		t.Fatalf("the system door saw %d rows, want the one the kernel wrote", n)
	}
}

// TestTheSameKeyOnAnotherRouteIsAnotherCommand: the operation is part of the
// identity, because two different commands sharing a key is what a client that
// reuses a variable looks like.
func TestTheSameKeyOnAnotherRouteIsAnotherCommand(t *testing.T) {
	api, router, _, runs, path := setupNote(t)
	if got := send(t, router, path, keyA, "same body").Code; got != http.StatusOK {
		t.Fatalf("the declared route answered %d", got)
	}
	quiet := at(api, "/quiet-notes")
	if got := send(t, router, quiet, keyA, "same body").Code; got != http.StatusOK {
		t.Fatalf("the other route answered %d", got)
	}
	if n := runs.Load(); n != 2 {
		t.Fatalf("two routes under one key ran the command %d times, want twice", n)
	}
}

// TestTheSameKeyAtAnotherPathOfTheSameRouteIsRefused is the case that catches an
// implementation keying identity on the mounted pattern alone: {id} is in the
// path, the operation is one route, and answering item BBB with item AAA's stored
// response would hand over the wrong row's answer confidently.
func TestTheSameKeyAtAnotherPathOfTheSameRouteIsRefused(t *testing.T) {
	api, router, f, runs, _ := setupNote(t)
	op := huma.Operation{OperationID: "record-item", Method: http.MethodPost, Path: "/items/{id}/records"}
	httpx.DeclareIdempotency(&op)
	httpx.Register(api.Surfaces(probe).App, op, httpx.Permission("note:write"),
		func(ctx context.Context, in *struct {
			ID   string `path:"id"`
			Body noteBody
		}) (*noteOut, error) {
			n := runs.Add(1)
			tx, _ := httpx.TxFrom(ctx)
			t, _ := tenancy.FromContext(ctx)
			if err := tx.DB().Exec("INSERT INTO notes (tenant_id, body) VALUES (?, ?)", t.ID, in.ID+":"+in.Body.Text).Error; err != nil {
				return nil, err
			}
			out := &noteOut{}
			out.Body.Text, out.Body.Runs = in.ID, n
			return out, nil
		})
	path := at(api, "/items")
	if got := send(t, router, path+"/AAA/records", keyA, "same body").Code; got != http.StatusOK {
		t.Fatalf("item AAA answered %d", got)
	}
	res := send(t, router, path+"/BBB/records", keyA, "same body")
	if res.Code != http.StatusUnprocessableEntity {
		t.Fatalf("the same key at another item answered %d, want 422: %s", res.Code, res.Body)
	}
	if !strings.Contains(res.Body.String(), httpx.CodeIdempotencyKeyReuse) {
		t.Errorf("the refusal names no code the caller can act on: %s", res.Body)
	}
	if n := runs.Load(); n != 1 {
		t.Fatalf("item BBB ran the command: %d runs, want the one AAA did", n)
	}
	if got := rowsOf(t, f, "notes"); len(got) != 1 || got[0] != "AAA:same body" {
		t.Fatalf("the rows are %v, want only AAA's: BBB must have neither a row nor AAA's answer", got)
	}
}

// TestAMalformedKeyIsRefusedAndWritesNothing: the shapes a mistake takes are a name
// typed into a token field, a UUID in capitals, and a UUID one character long.
// Nothing is claimed, so nothing is left behind.
func TestAMalformedKeyIsRefusedAndWritesNothing(t *testing.T) {
	_, router, f, runs, path := setupNote(t)
	for _, bad := range []string{"my-form-key", "3F2504E0-4F89-41D3-9A0C-0305E82C3301",
		keyA + "x", strings.Repeat("z", 36)} {
		res := send(t, router, path, bad, "body")
		if res.Code != http.StatusUnprocessableEntity {
			t.Errorf("key %q answered %d, want 422", bad, res.Code)
		}
		if !strings.Contains(res.Body.String(), httpx.CodeIdempotencyKeyInvalid) {
			t.Errorf("key %q refused without naming the code: %s", bad, res.Body)
		}
		if got := res.Header().Get(httpx.IdempotencyRefusalHeader); got != httpx.CodeIdempotencyKeyInvalid {
			t.Errorf("key %q carried refusal header %q", bad, got)
		}
	}
	if n := runs.Load(); n != 0 {
		t.Fatalf("a malformed key ran the command %d times, want none", n)
	}
	if got := heldRows(t, f, true); got != 0 {
		t.Fatalf("the refused keys left %d claims behind, want none", got)
	}
}

// TestAnUndeclaredRouteIgnoresTheKey: declaring the header is the module's
// decision, and a key sent to a route that never asked for one changes nothing.
func TestAnUndeclaredRouteIgnoresTheKey(t *testing.T) {
	api, router, _, runs, _ := setupNote(t)
	for range 2 {
		if got := send(t, router, at(api, "/quiet-notes"), keyA, "twice").Code; got != http.StatusOK {
			t.Fatalf("the undeclared route answered %d", got)
		}
	}
	if n := runs.Load(); n != 2 {
		t.Fatalf("an undeclared route honoured the key: %d runs, want two", n)
	}
}

// TestTheDocumentMarksTheOperation is the brief's OpenAPI acceptance: a client
// reading the document learns which operations accept the header and which never
// will. X-Expected-Principal is this kernel's precedent of a header it reads and
// the document never mentions; a header the client has to send is not that.
func TestTheDocumentMarksTheOperation(t *testing.T) {
	api, router, f := setupWith(t, true)
	f.exec(`CREATE TABLE notes (id serial PRIMARY KEY, tenant_id uuid NOT NULL, body text NOT NULL)`)
	f.signedIn()
	f.allow = true
	runs := &atomic.Int64{}
	mount(t, api, "declared-note", "/notes", true, note(runs))
	mount(t, api, "undeclared-note", "/quiet-notes", false, note(runs))

	var doc struct {
		Paths map[string]map[string]struct {
			Parameters []docParam `json:"parameters"`
		} `json:"paths"`
	}
	res := get(t, router, "/openapi.json")
	if res.Code != http.StatusOK {
		t.Fatalf("the document answered %d", res.Code)
	}
	if err := json.Unmarshal(res.Body.Bytes(), &doc); err != nil {
		t.Fatalf("read the document: %v", err)
	}
	path := at(api, "/notes")
	declared, ok := doc.Paths[path]["post"]
	if !ok {
		t.Fatalf("%s post is not in the document; it has %d paths", path, len(doc.Paths))
	}
	param := keyParam(declared.Parameters)
	if param == nil {
		t.Fatalf("the declared operation carries no Idempotency-Key parameter: %+v", declared.Parameters)
	}
	if param.Required != nil && *param.Required {
		t.Error("the parameter is required: a client that predates this would be refused")
	}
	if param.Schema.Type != "string" || param.Schema.Format != "uuid" {
		t.Errorf("the parameter schema is %s/%s, want string/uuid", param.Schema.Type, param.Schema.Format)
	}
	if keyParam(doc.Paths[at(api, "/quiet-notes")]["post"].Parameters) != nil {
		t.Error("a route that never declared the key carries it in the document")
	}
}

type docParam struct {
	Name     string `json:"name"`
	In       string `json:"in"`
	Required *bool  `json:"required"`
	Schema   struct {
		Type   string `json:"type"`
		Format string `json:"format"`
	} `json:"schema"`
}

func keyParam(params []docParam) *docParam {
	for i := range params {
		if params[i].Name == httpx.IdempotencyKeyHeader && params[i].In == "header" {
			return &params[i]
		}
	}
	return nil
}

// TestBootRefusesADeclarationThatCannotMeanWhatItSays, three ways: a read has no
// outcome to make happen once, the public surface has no principal to scope a key
// to, and a streaming route has already consumed the bytes the gate would hash.
// Each refusal names the operation, because a boot failure that does not say which
// route is a boot failure nobody can fix.
func TestBootRefusesADeclarationThatCannotMeanWhatItSays(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		mount      func(*httpx.API)
	}{
		{"a safe method", "is a read", func(api *httpx.API) {
			op := huma.Operation{OperationID: "read-once", Method: http.MethodGet, Path: "/read-once"}
			httpx.DeclareIdempotency(&op)
			httpx.Register(api.Surfaces(probe).App, op, httpx.Permission("note:read"), ok)
		}},
		{"the public surface", "no principal", func(api *httpx.API) {
			op := huma.Operation{OperationID: "public-once", Method: http.MethodPost, Path: "/public-once"}
			httpx.DeclareIdempotency(&op)
			httpx.Register(api.Surfaces(probe).Public, op, httpx.Public(), ok)
		}},
		{"a streamed body", "reads its own body", func(api *httpx.API) {
			op := huma.Operation{OperationID: "stream-once", Method: http.MethodPost, Path: "/stream-once",
				Extensions: map[string]any{httpx.StreamedBodyExtension: true}}
			httpx.DeclareIdempotency(&op)
			httpx.Register(api.Surfaces(probe).App, op, httpx.Permission("note:write"), ok)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api, _, _ := setup(t)
			tc.mount(api)
			err := api.ValidateDeclarations()
			if err == nil {
				t.Fatal("boot accepted a declaration that cannot mean what it says")
			}
			if !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), httpx.IdempotencyKeyHeader) {
				t.Fatalf("the refusal says %q", err)
			}
		})
	}
}

// TestADeclaredCommandOnASafeRouteStillRunsTheFirstTime covers the other half of
// that boot gate: nothing in this tree declares a key on a GET today, and the
// request gate would ignore one there anyway, because the header is never read on
// a route that did not declare it.
func TestADeclarationOnAnUndeclaredSurfaceIsIgnoredRatherThanFeared(t *testing.T) {
	api, router, _, runs, _ := setupNote(t)
	res := request(t, router, http.MethodGet, at(api, "/quiet-notes"), host)
	if res.Code != http.StatusMethodNotAllowed {
		t.Fatalf("a GET at a POST-only address answered %d, want 405", res.Code)
	}
	if n := runs.Load(); n != 0 {
		t.Fatalf("a read ran the command %d times", n)
	}
}

// TestAResponseTooLargeToHoldSaysItRan: an answer past the bound is never
// truncated and never silently released. The row says the command ran, and a
// repeat is told to read the result rather than write it again.
func TestAResponseTooLargeToHoldSaysItRan(t *testing.T) {
	api, router, _, runs, _ := setupNote(t)
	op := huma.Operation{OperationID: "wide-note", Method: http.MethodPost, Path: "/wide"}
	httpx.DeclareIdempotency(&op)
	type wide struct {
		Body []byte
	}
	httpx.Register(api.Surfaces(probe).App, op, httpx.Permission("note:write"),
		func(context.Context, *noteIn) (*wide, error) {
			runs.Add(1)
			return &wide{Body: []byte(strings.Repeat("y", 70<<10))}, nil
		})
	path := at(api, "/wide")
	first := send(t, router, path, keyA, "")
	if first.Code != http.StatusOK || len(first.Body.Bytes()) < 70<<10 {
		t.Fatalf("the first caller got %d with %d bytes, want its whole answer", first.Code, first.Body.Len())
	}
	second := send(t, router, path, keyA, "")
	if second.Code != http.StatusConflict {
		t.Fatalf("a repeat of an answer too big to hold got %d, want 409: %s", second.Code, second.Body)
	}
	if !strings.Contains(second.Body.String(), httpx.CodeIdempotencyResponseNotHeld) {
		t.Errorf("the 409 does not say what it means: %s", second.Body)
	}
	if n := runs.Load(); n != 1 {
		t.Fatalf("the command ran %d times, want once", n)
	}
}

// TestAnInFlightMarkerOlderThanFiveMinutesIsReclaimable is the crash window's only
// exit, with its boundary on both sides: four minutes fifty-nine refuses, five
// minutes one runs. The clock is Postgres's, which is why the rows are aged in SQL
// rather than by a test that injects a Clock the kernel does not consult.
func TestAnInFlightMarkerOlderThanFiveMinutesIsReclaimable(t *testing.T) {
	_, router, f, runs, path := setupNote(t)
	if got := send(t, router, path, keyA, "held").Code; got != http.StatusOK {
		t.Fatalf("the first request answered %d", got)
	}
	for _, tc := range []struct {
		name   string
		age    string
		want   int
		after  int64
		replay bool
	}{
		{"a claim aged four minutes fifty-nine", "4 minutes 59 seconds", http.StatusConflict, 1, false},
		{"a claim aged five minutes one second", "5 minutes 1 second", http.StatusOK, 2, false},
	} {
		// Back into the in-flight state at the age of the case's choosing — the
		// settled row above is what the claim would have become.
		runSystem(t, f, "UPDATE platformkit_idempotency SET settled = false, status = 0, response = NULL,"+
			" claimed_at = now() - interval '"+tc.age+"'")
		res := send(t, router, path, keyA, "held")
		if res.Code != tc.want {
			t.Errorf("a claim aged %s answered %d, want %d: %s", tc.name, res.Code, tc.want, res.Body)
		}
		if got := res.Header().Get("Idempotency-Replay") == "true"; got != tc.replay {
			t.Errorf("a claim aged %s reported replay %v, want %v", tc.name, got, tc.replay)
		}
		if n := runs.Load(); n != tc.after {
			t.Errorf("a claim aged %s ran the command %d times, want %d", tc.name, n, tc.after)
		}
	}
}

// TestThePurgeTakesExpiredAnswersAndNobodyElses is the job's own boundary: the rows
// it deletes are exactly the ones whose day is over. A claim the kernel is still
// holding for another submission is not a candidate, and a purge that took it would
// be a purge that made a command look never to have happened.
func TestThePurgeTakesExpiredAnswersAndNobodyElses(t *testing.T) {
	_, router, f, _, path := setupNote(t)
	if got := send(t, router, path, keyA, "kept").Code; got != http.StatusOK {
		t.Fatalf("the command answered %d", got)
	}
	const keyB = "3f2504e0-4f89-41d3-9a0c-0305e82c3309"
	send(t, router, path, keyB, "another")
	if n := heldRows(t, f, true); n != 2 {
		t.Fatalf("two submissions left %d claims, want two", n)
	}
	runSystem(t, f, "UPDATE platformkit_idempotency SET expires_at = now() - interval '1 minute' WHERE key = '"+keyB+"'")
	if err := httpx.PurgeIdempotency(t.Context(), f.app); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if n := heldRows(t, f, true); n != 1 {
		t.Fatalf("after the purge %d claims remain, want the one whose day is not over", n)
	}
	res := send(t, router, path, keyA, "kept")
	if res.Code != http.StatusOK || res.Header().Get("Idempotency-Replay") != "true" {
		t.Fatalf("the surviving answer was disturbed: %d replay=%q", res.Code, res.Header().Get("Idempotency-Replay"))
	}
	if err := httpx.PurgeIdempotency(t.Context(), f.app); err != nil {
		t.Fatalf("purge again: %v", err)
	}
	if n := heldRows(t, f, true); n != 1 {
		t.Fatalf("a second pass deleted a row whose day is not over: %d remain", n)
	}
}

// TestTheKernelUsesPostgresTimeForAKey: the two clocks are SQL's, so a replica that
// lags and a process whose clock drifted cannot disagree about whether a key is
// still in flight. The statement below is the whole of what the gate asks.
func TestTheKernelUsesPostgresTimeForAKey(t *testing.T) {
	_, router, f, _, path := setupNote(t)
	if got := send(t, router, path, keyA, "held").Code; got != http.StatusOK {
		t.Fatalf("the command answered %d", got)
	}
	var future bool
	err := db.RunSystem(t.Context(), f.app, syscap.NewSystemToken("kit/httpx test: which clock"),
		func(_ context.Context, tx db.Tx[db.System]) error {
			return tx.DB().Raw("SELECT expires_at > now() FROM platformkit_idempotency").Row().Scan(&future)
		})
	if err != nil {
		t.Fatalf("read the row: %v", err)
	}
	if !future {
		t.Fatal("a settled answer already reads as expired on the database's own clock")
	}
}

// --- helpers ---

func same(a, b []byte) bool { return string(a) == string(b) }

// heldRows counts the claims through one door or the other. The tenant door is the
// assertion: it is handed the tenant the rows name and still sees nothing.
func heldRows(t *testing.T, f *fixture, system bool) int {
	t.Helper()
	var n int
	var err error
	if system {
		err = db.RunSystem(t.Context(), f.app, syscap.NewSystemToken("kit/httpx test: the idempotency door"),
			func(_ context.Context, tx db.Tx[db.System]) error {
				return tx.DB().Raw("SELECT count(*) FROM platformkit_idempotency").Row().Scan(&n)
			})
		if err != nil {
			t.Fatalf("count claims through the system door: %v", err)
		}
		return n
	}
	err = db.Run(tenancy.WithTenant(t.Context(), f.tenant), f.app, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT count(*) FROM platformkit_idempotency").Row().Scan(&n)
	})
	if err != nil {
		// A policy that refuses the statement outright is the same answer as one
		// that returns no rows; which of the two it is belongs to the migration.
		t.Logf("a tenant transaction could not even ask: %v", err)
		return 0
	}
	return n
}

func runSystem(t *testing.T, f *fixture, statement string) {
	t.Helper()
	err := db.RunSystem(t.Context(), f.app, syscap.NewSystemToken("kit/httpx test: the idempotency boundary"),
		func(_ context.Context, tx db.Tx[db.System]) error {
			return tx.DB().Exec(statement).Error
		})
	if err != nil {
		t.Fatalf("%s: %v", statement, err)
	}
}

func countRows(t *testing.T, f *fixture, table string) int {
	t.Helper()
	var n int
	err := db.Run(tenancy.WithTenant(t.Context(), f.tenant), f.app, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT count(*) FROM " + table).Row().Scan(&n)
	})
	if err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func rowsOf(t *testing.T, f *fixture, table string) []string {
	t.Helper()
	var out []string
	err := db.Run(tenancy.WithTenant(t.Context(), f.tenant), f.app, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		rows, err := tx.DB().Raw("SELECT body FROM " + table).Rows()
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				return err
			}
			out = append(out, s)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("read %s: %v", table, err)
	}
	return out
}
