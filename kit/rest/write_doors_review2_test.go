package rest_test

// The door-level half of the review: what the folding rule and the new
// "changed nothing, say nothing" rule answer at the doors themselves — under
// every media type the create route accepts, for a row another tenant owns,
// when the refused body also named a field the caller was allowed to change,
// when two writes reach one row, and between the row a response describes and
// the row the database holds.

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// owned mounts the Spec with status and notes reserved to a route of their own,
// which is the shape every module that declares Immutable has.
func owned(t *testing.T) (chi.Router, *sql.DB) {
	t.Helper()
	s := spec
	s.Immutable = []string{"status", "notes"}
	_, router, admin := mount(t, s)
	return router, admin
}

// callCT is call with a Content-Type of the caller's, because the create route
// advertises more than application/json and the guard reads bytes rather than
// the decoded entity.
func callCT(t *testing.T, r http.Handler, method, path, body, contentType string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

// TestTheCreateDoorRefusesTheReservedNameUnderEveryMediaTypeItAdvertises. The
// guard is a read of the request bytes; the write is a decode of them. If one
// media type the route accepts reached the decoder without reaching the guard,
// the finding would be back in the shape of a header.
func TestTheCreateDoorRefusesTheReservedNameUnderEveryMediaTypeItAdvertises(t *testing.T) {
	router, admin := owned(t)
	for _, ct := range []string{
		"application/json",
		"application/json; charset=utf-8",
		"application/json;charset=UTF-8",
		"APPLICATION/JSON",
		"application/octet-stream",
		"text/plain",
		"application/merge-patch+json",
		"",
	} {
		// Did this media type reach the decoder at all? Asked of a body that
		// names nothing reserved: if it stores, the refusal below has to be a
		// refusal; if it does not, the case would pass with no door there.
		clean := `{"title":"clean ` + ct + `"}`
		code, out := callCT(t, router, http.MethodPost, "/api/tasks", clean, ct)
		if code != http.StatusCreated {
			t.Logf("Content-Type %q never reaches the decoder: %d %s", ct, code, out)
			continue
		}
		body := `{"title":"forged ` + ct + `","STATUS":"done"}`
		code, out = callCT(t, router, http.MethodPost, "/api/tasks", body, ct)
		if code == http.StatusCreated {
			t.Errorf("POST with Content-Type %q stored a row: 201 %s", ct, out)
		}
	}
	// No row reached the table with a status a command owns, whatever the
	// header said. The assertion is about the row, not the status: a 415 and a
	// 422 both refuse, and only the row says which.
	var n int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM rest_tasks WHERE status NOT IN ('', 'open')`).Scan(&n); err != nil {
		t.Fatalf("read the rows: %v", err)
	}
	if n != 0 {
		t.Errorf("%d rows carry a status nobody was allowed to set", n)
	}
}

// hosts is a tenant loader for two tenants, so one request can reach a row it
// has no business seeing.
type hosts struct{ byHost map[string]tenancy.Tenant }

func (h hosts) ByHost(_ context.Context, _ db.Tx[db.System], host string) (tenancy.Tenant, error) {
	tenant, ok := h.byHost[host]
	if !ok {
		return tenancy.Tenant{}, tenancy.ErrNoSuchHost
	}
	return tenant, nil
}
func (hosts) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) { return true, nil }

// TestWhatOneTenantCannotDoToAnotherTenantsRowThroughTheNewPath. "A merge that
// names no column returns the row as it was read" is a claim about a row this
// request is allowed to read; a body that names no column must not become the
// way a tenant reads a row it does not own.
func TestWhatOneTenantCannotDoToAnotherTenantsRowThroughTheNewPath(t *testing.T) {
	acme := tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}
	other := tenancy.Tenant{ID: uuid.New(), Slug: "other", Name: "Other"}
	admin, conn := dbtest.Schema(t)
	if _, err := admin.ExecContext(t.Context(), ddl); err != nil {
		t.Fatalf("create tasks: %v", err)
	}
	api, router := httpx.New(httpx.Options{
		PublicHost: host, Tenants: hosts{map[string]tenancy.Tenant{host: acme, "other.test": other}},
		Conn: conn, Authorize: caller{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: principal}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	s := spec
	s.Immutable = []string{"status"}
	s.Mount(api)
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted routes do not declare themselves: %v", err)
	}

	code, body := call(t, router, http.MethodPost, "/api/tasks", `{"title":"acme's own"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST = %d %s", code, body)
	}
	at := "/api/tasks/" + id(t, body)

	callOther := func(method, body string) (int, string) {
		req := httptest.NewRequest(method, "http://other.test"+at, strings.NewReader(body))
		req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w.Code, w.Body.String()
	}
	// The row exists as far as its own tenant is concerned; to the other one it
	// is nothing at all, and the empty body must not change that.
	for _, sent := range []string{`{}`, `{"title":"written across"}`, `{"status":"done"}`} {
		if code, out := callOther(http.MethodPatch, sent); code != http.StatusNotFound || strings.Contains(out, "acme's own") {
			t.Errorf("other tenant PATCH %s = %d %s, want 404 with nothing of the row in it", sent, code, out)
		}
	}
	if code, out := callOther(http.MethodGet, ""); code != http.StatusNotFound || strings.Contains(out, "acme's own") {
		t.Errorf("other tenant GET = %d %s, want 404 with nothing of the row in it", code, out)
	}
	// And nothing moved in the row or the outbox.
	var title, status string
	if err := admin.QueryRowContext(t.Context(),
		`SELECT title, status FROM rest_tasks WHERE tenant_id = $1`, acme.ID).Scan(&title, &status); err != nil {
		t.Fatalf("read acme's row: %v", err)
	}
	if title != "acme's own" || status != "" {
		t.Errorf("acme's row came back as %q/%q after the other tenant asked", title, status)
	}
	if n := count(t, admin, spec.Event(rest.Updated)); n != 0 {
		t.Errorf("the refused writes published %d %s events", n, spec.Event(rest.Updated))
	}
}

// TestARefusedPatchWritesNoneOfTheBody. The body that reaches the patch door
// names one field the caller owns and one it does not. The door refuses the
// write, which is the whole body: the mutable half is not applied on the way
// out, and the row the caller keeps is the row it had.
func TestARefusedPatchWritesNoneOfTheBody(t *testing.T) {
	router, admin := owned(t)
	code, body := call(t, router, http.MethodPost, "/api/tasks", `{"title":"before","priority":3}`)
	if code != http.StatusCreated {
		t.Fatalf("POST = %d %s", code, body)
	}
	at := "/api/tasks/" + id(t, body)

	if code, out := call(t, router, http.MethodPatch, at, `{"title":"after","STATUS":"done"}`); code != http.StatusUnprocessableEntity ||
		!strings.Contains(out, "status belongs to a route of its own") {
		t.Errorf(`PATCH {"title":"after","STATUS":"done"} = %d %s`, code, out)
	}
	var title string
	var priority int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT title, priority FROM rest_tasks WHERE id = $1`, at[strings.LastIndex(at, "/")+1:]).Scan(&title, &priority); err != nil {
		t.Fatalf("read the row: %v", err)
	}
	if title != "before" || priority != 3 {
		t.Errorf("the refused patch wrote the mutable half of its body: title %q, priority %d", title, priority)
	}
	if n := count(t, admin, spec.Event(rest.Updated)); n != 0 {
		t.Errorf("the refused patch published %d %s events", n, spec.Event(rest.Updated))
	}
}

// TestTwoWritesToOneRowNeitherLoseTheOthersColumnNorPublishTheOthersChange.
// The patch route locks the row, merges the columns the body named and writes
// those alone; the empty body now stops before the write, holding the lock. Two
// PATCHes of different columns have to survive together, and the one that named
// nothing has to say nothing even when it is the second one in.
func TestTwoWritesToOneRowNeitherLoseTheOthersColumnNorPublishTheOthersChange(t *testing.T) {
	router, admin := owned(t)
	code, body := call(t, router, http.MethodPost, "/api/tasks", `{"title":"contended"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST = %d %s", code, body)
	}
	at := "/api/tasks/" + id(t, body)

	var wg sync.WaitGroup
	codes := make([]int, 4)
	bodies := make([]string, 4)
	sent := []string{`{"title":"contended two"}`, `{}`, `{"priority":7}`, `{}`}
	for i, b := range sent {
		wg.Add(1)
		go func(i int, b string) {
			defer wg.Done()
			codes[i], bodies[i] = call(t, router, http.MethodPatch, at, b)
		}(i, b)
	}
	wg.Wait()
	for i, b := range sent {
		if codes[i] != http.StatusOK {
			t.Errorf("PATCH %s = %d %s, want 200", b, codes[i], bodies[i])
		}
	}
	var title string
	var priority int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT title, priority FROM rest_tasks WHERE id = $1`, at[strings.LastIndex(at, "/")+1:]).Scan(&title, &priority); err != nil {
		t.Fatalf("read the row: %v", err)
	}
	if title != "contended two" || priority != 7 {
		t.Errorf("the row holds title %q and priority %d; a write was lost", title, priority)
	}
	if n := count(t, admin, spec.Event(rest.Updated)); n != 2 {
		t.Errorf("four PATCHes published %d %s events, want the two that named a column", n, spec.Event(rest.Updated))
	}
}

// TestTheGuardSeesEveryBodyTheDecoderReads. The guard is a second read of the
// request bytes, so its answer is only worth anything where the decoder's read
// agrees: a body the guard cannot read and the decoder can is the finding again
// in the shape of a quote. Every shape below is answered either by the refusal
// naming the reserved field or by a refusal that stored no row — and the row is
// what the case asserts, not the sentence.
func TestTheGuardSeesEveryBodyTheDecoderReads(t *testing.T) {
	router, admin := owned(t)
	for _, sent := range []struct {
		what, body string
	}{
		{"the declared spelling", `{"title":"plain","STATUS":"done"}`},
		{"trailing space", `{"title":"space","STATUS":"done"} `},
		{"a second object after the first", `{"title":"twice","STATUS":"done"}{}`},
		{"garbage after the first", `{"title":"garbage","STATUS":"done"}nope`},
		{"a byte-order mark", "\ufeff" + `{"title":"bom","STATUS":"done"}`},
		{"leading whitespace", "   " + `{"title":"ws","STATUS":"done"}`},
		{"a trailing comma", `{"title":"comma","STATUS":"done",}`},
		{"the key twice, reserved last", `{"STATUS":"done","title":"dup"}`},
		{"the key twice, folded and exact", `{"status":"done","STATUS":"done","title":"dup2"}`},
		{"the reserved key as null", `{"title":"null","STATUS":null}`},
		{"an escaped key", `{"title":"escaped","\u0053TATUS":"done"}`},
	} {
		code, out := call(t, router, http.MethodPost, "/api/tasks", sent.body)
		if code == http.StatusCreated {
			t.Errorf("%s: POST stored a row: 201 %s", sent.what, out)
		}
	}
	// The reachability probe, independent of any refusal: a body naming only
	// writable fields still writes, so the count below is a door and not an
	// empty table.
	if code, out := call(t, router, http.MethodPost, "/api/tasks", `{"title":"what a body may say"}`); code != http.StatusCreated {
		t.Fatalf("the route refuses a body that names nothing reserved: %d %s", code, out)
	}
	var n int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM rest_tasks WHERE status NOT IN ('', 'open')`).Scan(&n); err != nil {
		t.Fatalf("read the rows: %v", err)
	}
	if n != 0 {
		t.Errorf("%d rows were stored with a status a command owns", n)
	}
}

// TestWhatARowIsAnsweredWithIsWhatTheRowHolds. The commit's case for moving
// GORM's clock onto db.Now is that a write answers with the instant the column
// keeps. Nothing else in the package reads the stored timestamp, so this is the
// only place the claim can be checked at all: the same instant, read back
// through SQL, both doors, and after a write that named a column.
func TestWhatARowIsAnsweredWithIsWhatTheRowHolds(t *testing.T) {
	_, router, admin := mounted(t)
	code, body := call(t, router, http.MethodPost, "/api/tasks", `{"title":"stamped"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST = %d %s", code, body)
	}
	at := "/api/tasks/" + id(t, body)
	asked := func(t *testing.T, body string) (created, updated time.Time) {
		t.Helper()
		var out struct {
			CreatedAt time.Time `json:"createdAt"`
			UpdatedAt time.Time `json:"updatedAt"`
		}
		if err := json.Unmarshal([]byte(body), &out); err != nil {
			t.Fatalf("read the stamps in %s: %v", body, err)
		}
		return out.CreatedAt, out.UpdatedAt
	}
	stored := func(t *testing.T) (created, updated time.Time) {
		t.Helper()
		var c, u time.Time
		if err := admin.QueryRowContext(t.Context(),
			`SELECT created_at, updated_at FROM rest_tasks WHERE id = $1`,
			at[strings.LastIndex(at, "/")+1:]).Scan(&c, &u); err != nil {
			t.Fatalf("read the stored stamps: %v", err)
		}
		return c.UTC().Truncate(time.Microsecond), u.UTC().Truncate(time.Microsecond)
	}
	created, updated := asked(t, body)
	sc, su := stored(t)
	if !created.Equal(sc) {
		t.Errorf("the create answered createdAt %s, the row holds %s", created.UTC(), sc)
	}
	if !updated.Equal(su) {
		t.Errorf("the create answered updatedAt %s, the row holds %s", updated.UTC(), su)
	}

	if code, body := call(t, router, http.MethodPatch, at, `{"title":"stamped twice"}`); code != http.StatusOK {
		t.Fatalf(`PATCH {"title":…} = %d %s`, code, body)
	} else {
		_, answered := asked(t, body)
		_, su = stored(t)
		if !answered.Equal(su) {
			t.Errorf("the patch answered updatedAt %s, the row holds %s", answered.UTC(), su)
		}
	}
	// The stamp is the kernel's clock, so a write cannot answer with an instant
	// older than the one before it, which is what an out-of-band clock costs.
	if su.Before(sc) {
		t.Errorf("updated_at %s precedes created_at %s", su, sc)
	}
}
