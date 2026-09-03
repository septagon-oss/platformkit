package file_test

import (
	"bytes"
	"context"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/contracts/filetest"
)

const (
	host   = "acme.test"
	files  = "/api/v1/file/files"
	public = "/api/v1/file/public/"
)

var acme = tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}

// png is the eight-byte signature of a real PNG, which is what
// http.DetectContentType reads and what the upload now checks a declared image
// against.
const png = "\x89PNG\r\n\x1a\n"

type caller struct{}

func (caller) ByHost(_ context.Context, _ db.Tx[db.System], h string) (tenancy.Tenant, error) {
	if h != host {
		return tenancy.Tenant{}, tenancy.ErrNoSuchHost
	}
	return acme, nil
}
func (caller) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) { return true, nil }

// mounted is the module as main mounts it, over a real Postgres and a real
// directory, with a limit small enough to go past in a test.
func mounted(t *testing.T) chi.Router {
	t.Helper()
	_, conn := dbtest.Schema(t)
	api, router := httpx.New(httpx.Options{
		PublicHost: host, Tenants: caller{}, Conn: conn, Authorize: caller{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New()}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	_, m := file.Module(file.Deps{Storage: file.Local(t.TempDir()), MaxBytes: filetest.Limit})
	m.Routes(api)
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted routes do not declare themselves: %v", err)
	}
	return router
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
	req := httptest.NewRequest(method, "http://"+host+at, nil)
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
	_, _ = file.Module(file.Deps{})
}

// TestTheManifestSubscribesToItsOwnDelete is what makes the bytes go: the
// module declares file.deleted and handles it, which is the only way work can
// be scheduled for after a commit.
func TestTheManifestSubscribesToItsOwnDelete(t *testing.T) {
	_, m := file.Module(file.Deps{Storage: filetest.NewMemory()})
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
	// not be cached is a logo served from this process forever.
	if got := header.Get("Cache-Control"); got != "" {
		t.Errorf("a public download says %q about caching, want nothing", got)
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
