package file_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/contracts/filetest"
)

const (
	host  = "acme.test"
	files = "/api/v1/file/files"
	// otherHost is the second tenant's origin, for the one case that needs two
	// tenants: what a stranger's session can reach at a door it was not given.
	otherHost = "globex.test"
	// The public door is on the public surface: an anonymous visitor's door has no
	// part of the tenant's workspace in its address. The address this door used to
	// answer at redirects, for one release, which is asserted below.
	public = "/api/v1/public/file/files/"
	// The address the public door used to answer at, for one release.
	wasPublic = "/api/v1/file/public/"
)

var acme = tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}

// globex is the other tenant, and it exists to be the one asking.
var globex = tenancy.Tenant{ID: uuid.New(), Slug: "globex", Name: "Globex"}

// outbox is the kernel's delivery table, named here because this package's tests
// read what a request left behind rather than only what it answered.
const outbox = "platformkit_outbox"

// png is the eight-byte signature of a real PNG, which is what
// http.DetectContentType reads and what the upload now checks a declared image
// against.
const png = "\x89PNG\r\n\x1a\n"

// caller is the tenant resolver and the authorizer both, in the shape httpx.New
// takes them. hosts is the map an origin resolves through: one entry for every
// case that needs one tenant, two for the case that asks what one tenant's session
// can reach inside another tenant's workspace.
type caller struct{ hosts map[string]tenancy.Tenant }

func (c caller) ByHost(_ context.Context, _ db.Tx[db.System], h string) (tenancy.Tenant, error) {
	resolved, ok := c.hosts[h]
	if !ok {
		return tenancy.Tenant{}, tenancy.ErrNoSuchHost
	}
	return resolved, nil
}
func (caller) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) { return true, nil }

// mounted is the module as main mounts it, over a real Postgres and a real
// directory, with a limit small enough to go past in a test.
func mounted(t *testing.T) chi.Router {
	t.Helper()
	router, _, _ := mountedOn(t, map[string]tenancy.Tenant{host: acme})
	return router
}

// mountedOn is that mount with the origins it answers on written down, and it
// hands back the two connections as well: admin to read what a request left in
// the tables, app for anything that has to be asked from a tenant's own
// transaction.
func mountedOn(t *testing.T, hosts map[string]tenancy.Tenant) (chi.Router, *sql.DB, *db.Conn) {
	t.Helper()
	admin, conn := dbtest.Schema(t, file.Migrations)
	api, router := httpx.New(httpx.Options{
		Cache:      cache.Memory("pkit"),
		PublicHost: host, Tenants: caller{hosts: hosts}, Conn: conn, Authorize: caller{hosts: hosts},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New()}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	_, m := file.New(file.Deps{Storage: file.Local(t.TempDir()), MaxBytes: filetest.Limit})
	m.Routes(surfacesOf(api))
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted routes do not declare themselves: %v", err)
	}
	return router, admin, conn
}

// upload posts one multipart form with one file part in it.
func upload(t *testing.T, r http.Handler, at, name, contentType, body string) (int, string) {
	t.Helper()
	var form bytes.Buffer
	w := multipart.NewWriter(&form)
	// CreateFormFile would label every part application/octet-stream, and the
	// media type is the thing the download answers with, so the part is built
	// with the header a browser would actually send.
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="file"; filename="`+name+`"`)
	header.Set("Content-Type", contentType)
	part, err := w.CreatePart(header)
	if err != nil {
		t.Fatalf("build the form: %v", err)
	}
	if _, err := part.Write([]byte(body)); err != nil {
		t.Fatalf("write the part: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close the form: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "http://"+host+at, &form)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func send(t *testing.T, r http.Handler, method, at string, signedIn bool) (int, string, http.Header) {
	t.Helper()
	return sendOn(t, r, host, method, at, signedIn)
}

// sendOn is the same request from another origin, which is the only way in this
// harness for one tenant's session to be somebody else's.
func sendOn(t *testing.T, r http.Handler, from, method, at string, signedIn bool) (int, string, http.Header) {
	t.Helper()
	req := httptest.NewRequest(method, "http://"+from+at, nil)
	if signedIn {
		req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String(), rec.Header()
}

// TestAFileGoesUpAndComesBackDown is the round trip: a multipart upload, the
// record, the bytes, and the headers a browser is told to trust.
func TestAFileGoesUpAndComesBackDown(t *testing.T) {
	router := mounted(t)
	const body = "hello, files\n"

	code, out := upload(t, router, files, "notes.txt", "text/plain", body)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", files, code, out)
	}
	id := field(t, out, "id")
	for _, want := range []string{`"name":"notes.txt"`, `"size":13`, `"sha256":"`, `"visibility":"private"`} {
		if !strings.Contains(out, want) {
			t.Errorf("the record does not carry %s: %s", want, out)
		}
	}
	if strings.Contains(out, "storageKey") {
		t.Errorf("the record carries its storage key, which is nobody's business: %s", out)
	}

	code, out, header := send(t, router, http.MethodGet, files+"/"+id+"/content", true)
	if code != http.StatusOK || out != body {
		t.Fatalf("GET the content = %d %q, want the bytes back", code, out)
	}
	switch {
	case header.Get("Content-Type") != "text/plain":
		t.Errorf("the download is typed %q", header.Get("Content-Type"))
	case header.Get("X-Content-Type-Options") != "nosniff":
		t.Error("the download lets a browser guess what it is")
	case !strings.Contains(header.Get("Content-Disposition"), "notes.txt"):
		t.Errorf("the download is named %q", header.Get("Content-Disposition"))
	// A private file is one tenant's own, and a browser or a proxy keeps what
	// nothing told it not to: the next person to ask that cache for this URL
	// must not be handed the bytes.
	case header.Get("Cache-Control") != "no-store":
		t.Errorf("a private download says %q about caching, want no-store", header.Get("Cache-Control"))
	}

	// The list, and then the delete: the record goes now, the bytes go when
	// whoever handles file.deleted gets to them.
	if code, out, _ = send(t, router, http.MethodGet, files, true); code != http.StatusOK || !strings.Contains(out, `"total":1`) {
		t.Errorf("GET %s = %d %s, want the one file", files, code, out)
	}
	if code, out, _ = send(t, router, http.MethodDelete, files+"/"+id, true); code != http.StatusNoContent {
		t.Fatalf("DELETE = %d %s, want 204", code, out)
	}
	if code, _, _ = send(t, router, http.MethodGet, files+"/"+id+"/content", true); code != http.StatusNotFound {
		t.Errorf("the deleted file's content = %d, want 404", code)
	}
}

// TestAnUploadPastTheLimitIsRefused. 413 is the one status this module has that
// nothing else does, and it is the only answer a caller can act on by sending
// something smaller.
func TestAnUploadPastTheLimitIsRefused(t *testing.T) {
	router := mounted(t)
	code, out := upload(t, router, files, "big.bin", "application/octet-stream", strings.Repeat("x", filetest.Limit+1))
	if code != http.StatusRequestEntityTooLarge {
		t.Errorf("an upload past the limit = %d %s, want 413", code, out)
	}
	// And exactly the limit is fine, which is what makes the boundary a
	// boundary rather than an approximation.
	if code, out = upload(t, router, files, "just.bin", "application/octet-stream", strings.Repeat("x", filetest.Limit)); code != http.StatusCreated {
		t.Errorf("an upload of exactly the limit = %d %s, want 201", code, out)
	}
	// A request that is not a form at all is the caller's mistake, not a 500.
	req := httptest.NewRequest(http.MethodPost, "http://"+host+files, strings.NewReader(`{"name":"notes.txt"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("an upload that is not a form = %d %s, want 422", rec.Code, rec.Body.String())
	}
}

// TestThePublicDoorServesOnlyPublicFiles: a private file and a file that does
// not exist are the same answer to a caller who is not signed in.
func TestThePublicDoorServesOnlyPublicFiles(t *testing.T) {
	router := mounted(t)

	code, out := upload(t, router, files, "secret.txt", "text/plain", "private things")
	if code != http.StatusCreated {
		t.Fatalf("POST = %d %s", code, out)
	}
	private := field(t, out, "id")

	code, out = upload(t, router, files+"?visibility=public", "logo.png", "image/png", png)
	if code != http.StatusCreated || !strings.Contains(out, `"visibility":"public"`) {
		t.Fatalf("POST a public file = %d %s", code, out)
	}
	open := field(t, out, "id")

	if code, out, _ = send(t, router, http.MethodGet, public+open, false); code != http.StatusOK || out != png {
		t.Errorf("the public file at the public door = %d %q", code, out)
	}
	if code, _, _ = send(t, router, http.MethodGet, public+private, false); code != http.StatusNotFound {
		t.Errorf("a private file at the public door = %d, want 404 and not 403", code)
	}
	if code, _, _ = send(t, router, http.MethodGet, public+uuid.NewString(), false); code != http.StatusNotFound {
		t.Errorf("a file that does not exist at the public door = %d, want the same 404", code)
	}
	// The private doors are still guarded.
	if code, _, _ = send(t, router, http.MethodGet, files+"/"+private+"/content", false); code != http.StatusForbidden {
		t.Errorf("an anonymous download = %d, want 403", code)
	}
	if code, _, _ = send(t, router, http.MethodGet, files, false); code != http.StatusForbidden {
		t.Errorf("an anonymous list = %d, want 403", code)
	}
}

// TestAModuleWithNoStorageDoesNotCompose: a wiring mistake fails where it is
// written, not on the first upload.
func TestAModuleWithNoStorageDoesNotCompose(t *testing.T) {
	defer func() {
		if r := recover(); r == nil || !strings.Contains(r.(string), "file.Local(dir)") {
			t.Errorf("Module with no storage panicked with %v; it names the one to wire", r)
		}
	}()
	_, _ = file.New(file.Deps{})
}

// TestTheManifestSubscribesToItsOwnDelete is what makes the bytes go: the
// module declares file.deleted and handles it, which is the only way work can
// be scheduled for after a commit.
func TestTheManifestSubscribesToItsOwnDelete(t *testing.T) {
	_, m := file.New(file.Deps{Storage: filetest.NewMemory()})
	if len(m.Subscriptions) != 1 || m.Subscriptions[0].Name != contracts.EventDeleted {
		t.Fatalf("the manifest subscribes to %v", m.Subscriptions)
	}
	if m.Subscriptions[0].Module != m.Name {
		t.Errorf("the subscription is attributed to %q and the module is %q", m.Subscriptions[0].Module, m.Name)
	}
}

func field(t *testing.T, body, name string) string {
	t.Helper()
	_, rest, ok := strings.Cut(body, `"`+name+`":"`)
	if !ok {
		t.Fatalf("no %s in %s", name, body)
	}
	out, _, _ := strings.Cut(rest, `"`)
	return out
}

// TestAnUploadedPageIsNeverServedInline is the fix for the review's finding
// that stored cross-site scripting was live: an uploaded text/html was served
// inline, anonymously, on the tenant's own origin, with no policy anywhere.
//
// Three things stop it now and each is checked here, because any one of them
// alone is one browser quirk away from failing: the disposition is attachment
// for anything outside the render-safe set, the policy allows nothing at all,
// and the resource policy stops another origin embedding it.
func TestAnUploadedPageIsNeverServedInline(t *testing.T) {
	router := mounted(t)
	const page = `<html><body><script>alert(document.cookie)</script></body></html>`

	code, out := upload(t, router, files+"?visibility=public", "evil.html", "text/html", page)
	if code != http.StatusCreated {
		t.Fatalf("POST = %d %s", code, out)
	}
	evil := field(t, out, "id")

	// The public door is the one an attacker sends a victim to.
	code, body, header := send(t, router, http.MethodGet, public+evil, false)
	switch {
	case code != http.StatusOK || body != page:
		t.Fatalf("the download = %d %q", code, body)
	case !strings.HasPrefix(header.Get("Content-Disposition"), "attachment"):
		t.Errorf("an uploaded page is served %q", header.Get("Content-Disposition"))
	case header.Get("Content-Security-Policy") != "default-src 'none'; sandbox":
		t.Errorf("the policy on a download is %q", header.Get("Content-Security-Policy"))
	case header.Get("Cross-Origin-Resource-Policy") != "same-site":
		t.Errorf("another origin may embed this: %q", header.Get("Cross-Origin-Resource-Policy"))
	case header.Get("X-Content-Type-Options") != "nosniff":
		t.Error("the download lets a browser guess what it is")
	}

	// SVG and XHTML are the two everybody forgets, and both execute script.
	for _, kind := range []string{"image/svg+xml", "application/xhtml+xml", "text/xml", "application/xml"} {
		code, out := upload(t, router, files, "x", kind, "<svg xmlns='http://www.w3.org/2000/svg'/>")
		if code != http.StatusCreated {
			t.Fatalf("POST %s = %d %s", kind, code, out)
		}
		_, _, h := send(t, router, http.MethodGet, files+"/"+field(t, out, "id")+"/content", true)
		if !strings.HasPrefix(h.Get("Content-Disposition"), "attachment") {
			t.Errorf("%s is served %q", kind, h.Get("Content-Disposition"))
		}
	}

	// And an image is still an image: a rule that made everything an
	// attachment would be a rule nobody could use a logo with.
	code, out = upload(t, router, files+"?visibility=public", "logo.png", "image/png", png)
	if code != http.StatusCreated {
		t.Fatalf("POST a png = %d %s", code, out)
	}
	_, _, header = send(t, router, http.MethodGet, public+field(t, out, "id"), false)
	if !strings.HasPrefix(header.Get("Content-Disposition"), "inline") {
		t.Errorf("a png is served %q", header.Get("Content-Disposition"))
	}
	// And a public file is deliberately cacheable: no-store belongs on the
	// responses that carry somebody's own data, and a public logo that could
	// not be cached is a logo served from this process forever. The public surface
	// gives this to a safe 2xx response that has no opinion of its own — the
	// module could name a longer life for an immutable upload, and would be
	// answered, but visibility is a thing that changes on an id, so it names
	// nothing and takes the surface's sixty seconds.
	if got := header.Get("Cache-Control"); got != "public, max-age=60" {
		t.Errorf("a public download says %q about caching, want it cacheable", got)
	}
}

// TestADownloadServesRangesAndAnswersHead. A media player asks for a range and
// a downloader asks for the size, and neither is something to implement twice:
// http.ServeContent is what answers both.
func TestADownloadServesRangesAndAnswersHead(t *testing.T) {
	router := mounted(t)
	const body = "0123456789"
	code, out := upload(t, router, files, "digits.txt", "text/plain", body)
	if code != http.StatusCreated {
		t.Fatalf("POST = %d %s", code, out)
	}
	at := files + "/" + field(t, out, "id") + "/content"

	req := httptest.NewRequest(http.MethodGet, "http://"+host+at, nil)
	req.Header.Set("Range", "bytes=2-5")
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	switch {
	case rec.Code != http.StatusPartialContent:
		t.Errorf("a range request = %d, want 206", rec.Code)
	case rec.Body.String() != "2345":
		t.Errorf("the range is %q, want the four bytes asked for", rec.Body.String())
	case rec.Header().Get("Content-Range") != "bytes 2-5/10":
		t.Errorf("Content-Range is %q", rec.Header().Get("Content-Range"))
	}

	code, head, header := send(t, router, http.MethodHead, at, true)
	switch {
	case code != http.StatusOK:
		t.Errorf("HEAD = %d, want 200", code)
	case head != "":
		t.Errorf("HEAD carried a body: %q", head)
	case header.Get("Content-Length") != "10":
		t.Errorf("HEAD says the file is %q bytes", header.Get("Content-Length"))
	case header.Get("Accept-Ranges") != "bytes":
		t.Error("the download does not say it serves ranges")
	}
}

// TestAnOverlongContentTypeIsTheCallersMistake. The column is varchar(120), so
// a header padded past it used to be a constraint violation the database raised
// and the caller read as a 500.
func TestAnOverlongContentTypeIsTheCallersMistake(t *testing.T) {
	router := mounted(t)
	code, out := upload(t, router, files, "x.bin", "application/"+strings.Repeat("x", 200), "bytes")
	if code != http.StatusUnprocessableEntity {
		t.Errorf("an overlong media type = %d %s, want 422", code, out)
	}
	// And one that is not a media type at all.
	if code, out = upload(t, router, files, "x.bin", "not a media type at all", "bytes"); code != http.StatusUnprocessableEntity {
		t.Errorf("a media type that is not one = %d %s, want 422", code, out)
	}
}

// surfacesOf is the module's view of the kernel: the three routers, named the
// way a composition names them at mount. The test keeps the *httpx.API
// separately, because validating the composition is the composition's job and
// holding a *Router would be holding one door of three.
func surfacesOf(a *httpx.API) httpx.Surfaces { return a.Surfaces("file") }

// ask sends one request with a JSON body and returns the answer.
func ask(t *testing.T, r http.Handler, method, at, body string) (int, string) {
	t.Helper()
	return askOn(t, r, host, method, at, body)
}

// askOn is ask with the origin it came from, for the case that needs a second
// tenant's session.
func askOn(t *testing.T, r http.Handler, from, method, at, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, "http://"+from+at, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// TestAHeldFileRefusesTheDeleteWithAConflict is the answer a caller gets when a
// hold stops it, at the door it knocked on.
//
// The refusal itself lived in the service and nowhere a caller could read it:
// fault() had no arm for contracts.ErrHeld, so rest.Fault — which knows the
// kernel's sentinels and none of this module's — handed it back unclassified and
// the caller read their own successful hold as a 500 outage. 409 is the sentence
// that means "these two things cannot both be true; go and release the other
// one", and a refused mutation writes nothing, which is checked here rather than
// asserted: the file the delete would have taken is still readable.
func TestAHeldFileRefusesTheDeleteWithAConflict(t *testing.T) {
	router := mounted(t)
	code, out := upload(t, router, files, "evidence.txt", "text/plain", "keep this")
	if code != http.StatusCreated {
		t.Fatalf("POST = %d %s", code, out)
	}
	id := field(t, out, "id")

	// 200 and not 201: this command places *or replaces*, and the answer is the
	// one hold there now, not the creation of a row.
	if code, out = ask(t, router, http.MethodPost, files+"/"+id+"/hold", `{"reason":"court order 2026-0412"}`); code != http.StatusOK {
		t.Fatalf("POST the hold = %d %s", code, out)
	}
	if !strings.Contains(out, "court order 2026-0412") {
		t.Errorf("the hold answers %s, which does not carry the reason it was placed for", out)
	}
	if code, out, _ = send(t, router, http.MethodDelete, files+"/"+id, true); code != http.StatusConflict {
		t.Errorf("DELETE a held file = %d %s, want 409 and not an outage", code, out)
	}
	if code, _, _ = send(t, router, http.MethodGet, files+"/"+id, true); code != http.StatusOK {
		t.Errorf("the file the refused delete would have taken reads %d, want it still there", code)
	}
	if code, out, _ = send(t, router, http.MethodDelete, files+"/"+id+"/hold", true); code != http.StatusNoContent {
		t.Fatalf("DELETE the hold = %d %s", code, out)
	}
	// Releasing a file with no hold is the success it is: the caller's intent is
	// already true, and refusing a write that found nothing is this
	// architecture's refusal, not its answer.
	if code, out, _ = send(t, router, http.MethodDelete, files+"/"+id+"/hold", true); code != http.StatusNoContent {
		t.Errorf("releasing twice = %d %s, want 204 both times", code, out)
	}
	if code, out, _ = send(t, router, http.MethodDelete, files+"/"+id, true); code != http.StatusNoContent {
		t.Errorf("the same delete after the release = %d %s, want 204", code, out)
	}
}

// TestAGrantIsRefusedBeforeTheStoreIsAsked pins the order of Grant's refusals,
// which is the whole reason one sentinel is not enough: the module's own ceiling
// and a public file are the request's to fix (422), and a deployment wired a
// store that cannot sign is neither — it is a wiring fact, answered 501 and not
// the 500 an operator would page about.
func TestAGrantIsRefusedBeforeTheStoreIsAsked(t *testing.T) {
	router := mounted(t)
	code, out := upload(t, router, files, "notes.txt", "text/plain", "private")
	if code != http.StatusCreated {
		t.Fatalf("POST a private file = %d %s", code, out)
	}
	private := field(t, out, "id")
	if code, out = upload(t, router, files+"?visibility=public", "logo.png", "image/png", png); code != http.StatusCreated {
		t.Fatalf("POST a public file = %d %s", code, out)
	}
	open := field(t, out, "id")

	// Past the module's 24h cap — refused and not clamped, before anyone asks
	// the store what it can do.
	if code, out, _ = send(t, router, http.MethodGet, files+"/"+private+"/grant?expires=25h", true); code != http.StatusUnprocessableEntity {
		t.Errorf("a 25h grant = %d %s, want 422 naming the expiry", code, out)
	}
	// A public file already has an open door; no signature teaches that signing
	// is where the access control lives. This is the same disk store, so the
	// refusal has to come first or it would never be seen.
	if code, out, _ = send(t, router, http.MethodGet, files+"/"+open+"/grant", true); code != http.StatusUnprocessableEntity {
		t.Errorf("a grant for a public file = %d %s, want 422", code, out)
	}
	// And the disk store says so out loud.
	if code, out, _ = send(t, router, http.MethodGet, files+"/"+private+"/grant", true); code != http.StatusNotImplemented {
		t.Errorf("a grant from a store that cannot sign = %d %s, want 501", code, out)
	}
}

// TestAnErasureNamesTheSubjectOrIsRefused is the other half of the same route,
// and the reason it is written as a receipt rather than a removal: the harness
// gives every request its own principal, so this is the one way to see that the
// command read its body at all. A body nobody read is a zero uuid, and a zero
// subject is a refusal — so the answer that names the subject back is the proof
// that the bytes on the wire arrived.
func TestAnErasureNamesTheSubjectOrIsRefused(t *testing.T) {
	router := mounted(t)
	if code, out := upload(t, router, files, "notes.txt", "text/plain", "hello"); code != http.StatusCreated {
		t.Fatalf("POST = %d %s", code, out)
	}
	subject := uuid.NewString()

	code, out := ask(t, router, http.MethodPost, files+"/erase", `{"subject":"`+subject+`","reason":"data protection request"}`)
	if code != http.StatusOK {
		t.Fatalf("POST an erasure of a subject with nothing here = %d %s", code, out)
	}
	if !strings.Contains(out, `"subject":"`+subject+`"`) || !strings.Contains(out, `"files":0`) {
		t.Errorf("the receipt reads %s, which does not name the subject it answered for", out)
	}
	// An erasure of nobody is the request's to fix, and says so rather than
	// removing everything this tenant happens to hold.
	if code, out = ask(t, router, http.MethodPost, files+"/erase", `{"reason":"data protection request"}`); code != http.StatusUnprocessableEntity {
		t.Errorf("an erasure that names no subject = %d %s, want 422", code, out)
	}
	// And the file that subject did not upload is where it was.
	if code, _, _ = send(t, router, http.MethodGet, files, true); code != http.StatusOK {
		t.Errorf("the list after an erasure of nothing = %d", code)
	}
}

// TestATenantCannotGrantOrEraseAnothersFile is the isolation claim at the door a
// stranger actually knocks on. The port's own suite proves one tenant's key opens
// nothing in the store, and the service tests prove one tenant's transaction reads
// no other's row; what nothing proved until now is that a second tenant's *HTTP
// session* gets nothing at a first tenant's file id — the assertion the register
// asks to be named, and the one a route could get wrong while every test above
// stayed green (a handler that read the row before it scoped the transaction would
// have had no case against it).
//
// The shape of the answer matters as much as the refusal. A file that exists in
// another tenant's workspace has to answer exactly as a file that does not exist
// here at all: the same 404, and a body that names none of it. "That id is
// somebody else's" is a fact about the other tenant's workspace, and an answer
// that explains itself leaks what the request never had to know.
//
// The erasure is the widest door of the three — it deletes by a subject rather
// than by an id — so it is asked for acme's own uploader, and the answer has to be
// a receipt of zero rather than an error, with nothing written in anybody's table.
func TestATenantCannotGrantOrEraseAnothersFile(t *testing.T) {
	router, admin, _ := mountedOn(t, map[string]tenancy.Tenant{host: acme, otherHost: globex})
	code, out := upload(t, router, files, "diary.txt", "text/plain", "something this person wrote")
	if code != http.StatusCreated {
		t.Fatalf("acme's upload = %d %s", code, out)
	}
	var row struct {
		ID       uuid.UUID `json:"id"`
		Uploader uuid.UUID `json:"uploader"`
	}
	if err := json.Unmarshal([]byte(out), &row); err != nil {
		t.Fatalf("the uploaded record is not the shape the route declares: %v (%s)", err, out)
	}
	if row.Uploader == uuid.Nil {
		t.Fatalf("the uploaded record names nobody: %s", out)
	}

	for _, door := range []struct{ method, at, why string }{
		{http.MethodGet, files + "/" + row.ID.String() + "/content", "a download"},
		{http.MethodGet, files + "/" + row.ID.String() + "/grant", "a signed URL"},
		{http.MethodDelete, files + "/" + row.ID.String(), "a delete"},
	} {
		code, out, _ := sendOn(t, router, otherHost, door.method, door.at, true)
		if code != http.StatusNotFound {
			t.Errorf("globex asked for %s of acme's file = %d %s, want the 404 an absent file gets",
				door.why, code, out)
		}
		// The refusal is about the request, not about what it found.
		if strings.Contains(out, "diary.txt") || strings.Contains(out, row.ID.String()) {
			t.Errorf("the refusal of %s names what it refused: %s", door.why, out)
		}
	}
	// Which is the same answer acme gets for an id nobody ever minted: one status,
	// one shape, and no arm of the handler that knows the difference.
	code, absent, _ := sendOn(t, router, otherHost, http.MethodGet,
		files+"/"+uuid.NewString()+"/content", true)
	if code != http.StatusNotFound {
		t.Errorf("an id never minted = %d %s, want 404", code, absent)
	}

	// The subject erasure, asked by the tenant that holds none of those files.
	code, out = askOn(t, router, otherHost, http.MethodPost, files+"/erase",
		`{"subject":"`+row.Uploader.String()+`","reason":"data protection request 2026-0412"}`)
	if code != http.StatusOK {
		t.Fatalf("globex erasing acme's subject = %d %s, want the receipt of nothing", code, out)
	}
	if !strings.Contains(out, `"files":0`) {
		t.Errorf("the receipt across the tenant boundary reads %s, want zero files erased", out)
	}

	// Read at the tables and not at the response: a refusal that wrote a work
	// order, a proof row or a soft delete would still have answered zero here.
	var erased, orders int
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM file_erasures`).Scan(&erased); err != nil {
		t.Fatalf("read the proofs: %v", err)
	}
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM `+outbox+` WHERE name IN ($1, $2)`,
		contracts.EventDeleted, contracts.EventErased).Scan(&orders); err != nil {
		t.Fatalf("read the outbox: %v", err)
	}
	if erased != 0 || orders != 0 {
		t.Errorf("the cross-tenant erasure wrote %d proof rows and %d removal events, want neither",
			erased, orders)
	}

	// And acme's file is where it was, at the same doors.
	if code, out, _ = send(t, router, http.MethodGet, files+"/"+row.ID.String()+"/content", true); code != http.StatusOK ||
		out != "something this person wrote" {
		t.Errorf("acme's own download after the other tenant's attempt = %d %s", code, out)
	}
}

// TestAnErasureAtTheDoorFilesItsReasonAndRefusesOneThatDoesNotFit is the sentence
// a person typed travelling from the request body to the record — and the ceiling
// on it, tested where a caller would hit it.
//
// The service tests prove the command files a reason; nothing proved that the
// reason arrives at the command. `POST /files/erase` is the only way one can reach
// `EraseSubject` with a sentence in it, and this route is the one this module has
// already gotten wrong once: eraseBody's fields were flat on the input struct
// beside a path tag, which kit/rest reads as no body at all, so every erasure ran
// as though the caller had sent nothing and answered a receipt of zero. A body
// that arrives is half of "the reason is kept"; the other half is that it is kept
// somewhere a person can be asked for it, so the case reads the outbox row the
// command committed rather than only the receipt it answered with. The bytes go
// after this transaction, which is exactly why the work order is the record a
// door-level case can read.
//
// The over-long reason is the second half and the reason the first half is not
// enough on its own. Both tables cap the sentence — contracts.MaxErasureReason on
// the service side, maxLength on the schema — and a cap nobody sends a request
// past is a comment. What has to be true is that the ceiling refuses rather than
// truncates: a record with the first 500 characters of a data-protection sentence
// is a record of a conversation nobody had, and it is unfalsifiable from the
// receipt, which would read files:1 either way. 422 is what every other refusal
// on this door answers, and the refusal has to write nothing, which is read at the
// file and at the outbox rather than assumed.
//
// The hold is the last leg because it is the same decision pointed the other way:
// file_holds.reason is the column this module cites when it argues that a removal
// is a decision somebody said a reason for. Both doors now state the column's two
// bounds in their schemas — not empty, at most 500 — and both refuse a request
// past either without leaving a row, so the argument holds for both.
func TestAnErasureAtTheDoorFilesItsReasonAndRefusesOneThatDoesNotFit(t *testing.T) {
	router, admin, _ := mountedOn(t, map[string]tenancy.Tenant{host: acme})

	// Whoever this upload answers for is the subject: the harness gives every
	// request its own principal, and the record names it back.
	code, out := upload(t, router, files, "diary.txt", "text/plain", "something this person wrote")
	if code != http.StatusCreated {
		t.Fatalf("POST = %d %s", code, out)
	}
	diary, subject := field(t, out, "id"), field(t, out, "uploader")

	const reason = "data protection request 2026-0412"
	if code, out = ask(t, router, http.MethodPost, files+"/erase",
		`{"subject":"`+subject+`","reason":"`+reason+`"}`); code != http.StatusOK {
		t.Fatalf("POST the erasure = %d %s", code, out)
	} else if !strings.Contains(out, `"files":1`) {
		t.Fatalf("the receipt reads %s, want the one file this subject uploaded", out)
	}

	var filed, cause string
	if err := admin.QueryRowContext(t.Context(),
		`SELECT coalesce(payload->>'reason', ''), coalesce(payload->>'cause', '') FROM `+outbox+` WHERE name = $1`,
		contracts.EventDeleted).Scan(&filed, &cause); err != nil {
		t.Fatalf("read the removal order this erasure left: %v", err)
	}
	if filed != reason {
		t.Errorf("the work order carries reason %q, want the sentence sent at the door (%q)", filed, reason)
	}
	if cause != contracts.EraseSubject {
		t.Errorf("the work order names cause %q, want %q: the reason is a sentence, the cause is which kind of removal this was",
			cause, contracts.EraseSubject)
	}
	// The file it removed is gone from the list the same session reads.
	if code, _, _ = send(t, router, http.MethodGet, files+"/"+diary, true); code != http.StatusNotFound {
		t.Errorf("the erased file reads %d, want 404", code)
	}

	// Past the ceiling: refused, and nothing written anywhere.
	code, out = upload(t, router, files, "second.txt", "text/plain", "a second subject")
	if code != http.StatusCreated {
		t.Fatalf("POST = %d %s", code, out)
	}
	second, other := field(t, out, "id"), field(t, out, "uploader")
	if code, out = ask(t, router, http.MethodPost, files+"/erase", `{"subject":"`+other+
		`","reason":"`+strings.Repeat("x", contracts.MaxErasureReason+1)+`"}`); code != http.StatusUnprocessableEntity {
		t.Errorf("an erasure whose reason is %d characters = %d %s, want 422",
			contracts.MaxErasureReason+1, code, out)
	}
	if code, _, _ = send(t, router, http.MethodGet, files+"/"+second, true); code != http.StatusOK {
		t.Errorf("the file the refused erasure would have taken reads %d, want it still there", code)
	}
	var orders int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM `+outbox+` WHERE name IN ($1, $2)`,
		contracts.EventDeleted, contracts.EventErased).Scan(&orders); err != nil {
		t.Fatalf("count the removal orders: %v", err)
	}
	if orders != 1 {
		t.Errorf("the refused erasure published %d removal orders alongside the accepted one, want none", orders)
	}

	// The ceiling is the ceiling, and not one character short of it: a reason of
	// exactly MaxErasureReason is a request this module accepts and files, so what
	// refused above is the width and not an off-by-one.
	if code, out = ask(t, router, http.MethodPost, files+"/erase", `{"subject":"`+other+
		`","reason":"`+strings.Repeat("y", contracts.MaxErasureReason)+`"}`); code != http.StatusOK {
		t.Errorf("an erasure whose reason is exactly %d characters = %d %s, want it accepted and filed",
			contracts.MaxErasureReason, code, out)
	}

	// The same ceiling on the decision pointed the other way.
	code, out = upload(t, router, files, "keep.txt", "text/plain", "held")
	if code != http.StatusCreated {
		t.Fatalf("POST = %d %s", code, out)
	}
	heldFile := field(t, out, "id")
	if code, out = ask(t, router, http.MethodPost, files+"/"+heldFile+"/hold",
		`{"reason":"`+strings.Repeat("z", contracts.MaxHoldReason+1)+`"}`); code != http.StatusUnprocessableEntity {
		t.Errorf("a hold whose reason is %d characters = %d %s, want 422",
			contracts.MaxHoldReason+1, code, out)
	}
	// The other half of the column's own CHECK: a hold with no reason is not a
	// hold at all, and the door says so rather than placing an unjustified one.
	if code, out = ask(t, router, http.MethodPost, files+"/"+heldFile+"/hold", `{"reason":""}`); code != http.StatusUnprocessableEntity {
		t.Errorf("a hold placed for no reason = %d %s, want 422", code, out)
	}
	var holds int
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM file_holds`).Scan(&holds); err != nil {
		t.Fatalf("count the holds: %v", err)
	}
	if holds != 0 {
		t.Errorf("the two refused holds wrote %d rows, want none", holds)
	}
	if code, out = ask(t, router, http.MethodPost, files+"/"+heldFile+"/hold",
		`{"reason":"`+strings.Repeat("w", contracts.MaxHoldReason)+`"}`); code != http.StatusOK {
		t.Errorf("a hold whose reason is exactly %d characters = %d %s, want it placed",
			contracts.MaxHoldReason, code, out)
	}
}
