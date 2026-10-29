package file_test

import (
	"bytes"
	"context"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strconv"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/limit"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts/filetest"
)

// TestAnUploadPastOnePersonsAllowanceIsRefused counts uploads at the module's own
// route over the kernel's Postgres counter, the way kit/app composes it: one
// person's minute of uploads is spent, the next one answers 429 with a
// Retry-After and keeps no row, and a second person of the same tenant is still
// let through.
func TestAnUploadPastOnePersonsAllowanceIsRefused(t *testing.T) {
	admin, conn := dbtest.Schema(t, file.Migrations)
	person := uuid.New()
	api, router := httpx.New(httpx.Options{
		SystemAccess: declaredCrossTenantAccess(),
		Cache:        cache.Memory("pkit"),
		PublicHost:   host, Tenants: caller{hosts: map[string]tenancy.Tenant{host: acme}}, Conn: conn,
		Authorize: caller{hosts: map[string]tenancy.Tenant{host: acme}},
		Authenticate: func(_ context.Context, _ db.Tx[db.Tenant], r *http.Request) (tenancy.Principal, bool, error) {
			if r.Header.Get("X-Second-Person") != "" {
				return tenancy.Principal{UserID: uuid.New()}, true, nil
			}
			return tenancy.Principal{UserID: person}, true, nil
		},
		WriteLimiter: limit.Postgres(func(context.Context) (*db.Conn, bool) { return conn, true }),
		Log:          slog.New(slog.DiscardHandler),
	})
	_, m := file.New(file.Deps{Storage: file.Local(t.TempDir()), MaxBytes: filetest.Limit})
	m.Routes(surfacesOf(api))
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	stored := func() int {
		t.Helper()
		var n int
		if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM files").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	accepted := 0
	for range 200 {
		code, out := upload(t, router, files, "note.txt", "text/plain", "a note\n")
		if code == http.StatusCreated {
			accepted++
			continue
		}
		if code != http.StatusTooManyRequests {
			t.Fatalf("upload %d = %d %s, want 201 until the allowance is spent, then 429", accepted+1, code, out)
		}
		break
	}
	if accepted == 0 || accepted >= 200 {
		t.Fatalf("one person's uploads accepted = %d, want a bounded allowance", accepted)
	}
	if got := stored(); got != accepted {
		t.Errorf("files kept = %d after %d accepted uploads and one refused, want the refused one to keep nothing", got, accepted)
	}

	req, rec := uploadRequest(t, "note.txt", "text/plain", "a note\n")
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("the same person again = %d, want 429", rec.Code)
	}
	if after, err := strconv.Atoi(rec.Header().Get("Retry-After")); err != nil || after < 1 {
		t.Errorf("a refused upload's Retry-After = %q, want a whole number of seconds", rec.Header().Get("Retry-After"))
	}

	req, rec = uploadRequest(t, "note.txt", "text/plain", "a note\n")
	req.Header.Set("X-Second-Person", "yes")
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Errorf("another person of the same tenant = %d %s, want 201", rec.Code, rec.Body.String())
	}
}

// uploadRequest is upload's request before it is served, for a case that has to
// read the answer's headers or set one of its own.
func uploadRequest(t *testing.T, name, contentType, body string) (*http.Request, *httptest.ResponseRecorder) {
	t.Helper()
	var form bytes.Buffer
	w := multipart.NewWriter(&form)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="file"; filename="`+name+`"`)
	header.Set("Content-Type", contentType)
	part, err := w.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "http://"+host+files, &form)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	return req, httptest.NewRecorder()
}
