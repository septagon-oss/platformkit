package httpx_test

// ask_test.go is the kernel's own suite for the port it declares: Access, the
// reach an ask needs to be more than a row.
//
// The reach is three other modules' rows seen from one composition — auth knows
// which roles manage roles, user knows who holds them, notification owns the bell
// — so the only implementation in this repository sits in a product, and until
// this file nothing in kit/httpx had ever called Ask. A rule the command enforces
// (an outage is never an allowance, a failed notice is never a half-written ask,
// the record may claim only what was written) was therefore priced at the layer
// that owns it by whoever happened to write an application test first.
//
// reachF is also the fake every port is owed: a composition that wants to drive an
// ask in a test of its own records what it was asked to tell instead of wiring a
// database, and the one real implementation and this double are answerable to the
// same nine assertions below.

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// reachF is the httpx.AskForAccess a test drives: it answers who to tell and how,
// records what it was told to say, and can fail at either half.
type reachF struct {
	recipients []uuid.UUID
	recErr     error
	tellErr    error
	// failOn is the 1-based call number on which Tell answers tellErr, which is how
	// a case asks whether a notice already written in the same transaction survives
	// the one that failed.
	failOn int

	told []httpx.AccessNotice
}

func (r *reachF) Recipients(context.Context, db.Tx[db.Tenant]) ([]uuid.UUID, error) {
	return r.recipients, r.recErr
}

func (r *reachF) Tell(_ context.Context, _ db.Tx[db.Tenant], n httpx.AccessNotice) error {
	if r.tellErr != nil && len(r.told)+1 == r.failOn {
		return r.tellErr
	}
	r.told = append(r.told, n)
	return nil
}

// limiterF is the counting the door is handed. The default allows; errF makes the
// counter an outage, and allowed=false makes the limit spent.
type limiterF struct {
	allowed bool
	errF    error
}

func (l limiterF) Allow(context.Context, string, int, time.Duration) (bool, time.Duration, error) {
	if l.errF != nil {
		return false, 0, l.errF
	}
	return l.allowed, time.Minute, nil
}

// askSetup mounts one signed-in door that calls the command, over the same
// fixture every file in this package uses, with a reach and a limiter the case
// names. A nil limiter leaves the door uncounted, which is what a composition
// without kit/limit gets.
func askSetup(t *testing.T, reach httpx.AskForAccess, lim httpx.WriteLimiter) (http.Handler, string, *fixture, *[]httpx.AccessRecord) {
	t.Helper()
	_, app := dbtest.Schema(t)
	f := &fixture{
		tenant: tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"},
		app:    app,
		logs:   &lines{},
	}
	records := &[]httpx.AccessRecord{}
	api, router := httpx.New(httpx.Options{
		Installation: host,
		PublicHost:   host,
		Tenants:      f,
		Conn:         app,
		Authorize:    f,
		Entitle:      f,
		Authenticate: f.authenticate,
		Log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		Access:       reach,
		Accessed: func(_ context.Context, rec httpx.AccessRecord) error {
			*records = append(*records, rec)
			return nil
		},
		WriteLimiter: lim,
	})
	// The catalogue the command checks the asked permission against: a key nobody
	// declares names no grant, and this is the only place one is declared here.
	api.Declare([]tenancy.Grant{{Permission: "widget:read", Label: "read widgets"}})
	httpx.Register(api.Surfaces(probe).App, huma.Operation{
		OperationID: "ask-widget", Method: http.MethodPost, Path: "/asks",
		DefaultStatus: http.StatusAccepted,
	}, httpx.SignedIn(), func(ctx context.Context, in *askInput) (*struct{}, error) {
		return nil, httpx.Ask(ctx, httpx.AccessAsk{Permission: in.Body.Permission, Path: in.Body.Path})
	})
	return router, at(api, "/asks"), f, records
}

type askInput struct {
	Body struct {
		Permission string `json:"permission"`
		Path       string `json:"path,omitempty"`
	} `required:"true"`
}

// askOnce sends one ask. The cookie is what makes the identity hook answer at all
// (see the note on request in httpx_test.go), and no Sec-Fetch-Site or Origin is
// what a non-browser client sends.
func askOnce(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "http://"+host+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// TestTheAskCommandAnswersEveryVerdictItsOwnCodeCanProduce walks the command the
// way the authorizer suite walks an authorization: every answer it can give, and
// what it left behind in each. The assertions are about the notices and the record,
// never about a sentence, so a reworded refusal cannot hide a write that happened.
func TestTheAskCommandAnswersEveryVerdictItsOwnCodeCanProduce(t *testing.T) {
	const asked = `{"permission":"widget:read","path":"/app/widget/widgets"}`
	other, asker := uuid.New(), uuid.New()

	t.Run("the notices it wrote, and only those", func(t *testing.T) {
		// The asker is in the recipient list — the reach answers who holds the
		// grant, and that is the asker too — and is never told their own ask. What
		// the record carries is the number of notices, not the size of the list.
		r := &reachF{recipients: []uuid.UUID{asker, other, uuid.New()}}
		h, path, f, records := askSetup(t, r, limiterF{allowed: true})
		f.signedIn()
		f.principal = &tenancy.Principal{UserID: asker}
		f.allow = true

		res := askOnce(t, h, path, asked)
		if res.Code != http.StatusAccepted {
			t.Fatalf("POST the ask = %d %s, want it accepted", res.Code, res.Body)
		}
		if len(r.told) != 2 {
			t.Fatalf("the ask wrote %d notices of %d recipients, want the asker skipped: %v",
				len(r.told), len(r.recipients), r.told)
		}
		for _, n := range r.told {
			if n.To == asker {
				t.Errorf("the asker was told about their own ask")
			}
			if n.Requester != asker || n.Permission != "widget:read" || n.Label != "read widgets" ||
				n.RefusedPath != "/app/widget/widgets" {
				t.Errorf("the notice names %q rather than the ask: %+v", n.To, n)
			}
		}
		if len(*records) != 1 || (*records)[0].Notified != 2 {
			t.Errorf("the record claims %v for %d notices written; notified counts deliveries, not recipients",
				*records, len(r.told))
		}
	})

	t.Run("a tenant with nobody to tell is answered, not refused", func(t *testing.T) {
		r := &reachF{}
		h, path, f, records := askSetup(t, r, limiterF{allowed: true})
		f.signedIn()
		f.allow = true

		if res := askOnce(t, h, path, asked); res.Code != http.StatusAccepted {
			t.Fatalf("POST the ask where nobody holds role management = %d %s, want it recorded", res.Code, res.Body)
		}
		if len(*records) != 1 || (*records)[0].Notified != 0 || len(r.told) != 0 {
			t.Errorf("an empty answer was recorded as %v with %d notices; an empty answer is a fact, not an outage",
				*records, len(r.told))
		}
	})

	t.Run("a permission nobody declares names no grant", func(t *testing.T) {
		r := &reachF{recipients: []uuid.UUID{other}}
		h, path, f, records := askSetup(t, r, limiterF{allowed: true})
		f.signedIn()
		f.allow = true

		res := askOnce(t, h, path, `{"permission":"nope:read"}`)
		if res.Code != http.StatusUnprocessableEntity || len(r.told) != 0 || len(*records) != 0 {
			t.Errorf("asking for an undeclared key = %d, %d notices, %d records; want 422 and nothing written: %s",
				res.Code, len(r.told), len(*records), res.Body)
		}
	})

	t.Run("a link is a path in this application", func(t *testing.T) {
		r := &reachF{recipients: []uuid.UUID{other}}
		h, path, f, records := askSetup(t, r, limiterF{allowed: true})
		f.signedIn()
		f.allow = true

		res := askOnce(t, h, path, `{"permission":"widget:read","path":"https://elsewhere.test/grant-me"}`)
		if res.Code != http.StatusUnprocessableEntity || len(r.told) != 0 || len(*records) != 0 {
			t.Errorf("an absolute link in the ask = %d, %d notices, %d records; want 422 and nothing written: %s",
				res.Code, len(r.told), len(*records), res.Body)
		}
	})

	t.Run("a reach that cannot answer is an outage", func(t *testing.T) {
		r := &reachF{recErr: errors.New("the roles table is not answering")}
		h, path, f, records := askSetup(t, r, limiterF{allowed: true})
		f.signedIn()
		f.allow = true

		res := askOnce(t, h, path, asked)
		if res.Code != http.StatusServiceUnavailable {
			t.Errorf("POST the ask with a failing reach = %d %s, want 503", res.Code, res.Body)
		}
		if len(*records) != 0 {
			t.Errorf("the trail was written for an ask that never reached anybody: %v", *records)
		}
	})

	t.Run("a notice that fails leaves no half-written ask", func(t *testing.T) {
		// Three recipients, the second Tell failing. The first notice is in the same
		// transaction as the record, so what a case can demand is that the record
		// never happens: the caller's transaction rolls the notice back, and an
		// event written first would be a trail beside a bell that dropped it.
		r := &reachF{recipients: []uuid.UUID{asker, other, uuid.New()},
			tellErr: errors.New("the mailbox is closed"), failOn: 2}
		h, path, f, records := askSetup(t, r, limiterF{allowed: true})
		f.signedIn()
		f.principal = &tenancy.Principal{UserID: asker}
		f.allow = true

		res := askOnce(t, h, path, asked)
		if res.Code < 400 {
			t.Errorf("the ask answered %d although the second notice failed: %s", res.Code, res.Body)
		}
		if len(r.told) < 1 {
			t.Fatalf("this case needs a notice written before the failure; Tell was reached %d times", len(r.told))
		}
		if len(*records) != 0 {
			t.Errorf("the record happened for an ask whose notices failed: %v", *records)
		}
	})

	t.Run("a counter that cannot be reached is never an allowance", func(t *testing.T) {
		r := &reachF{recipients: []uuid.UUID{other}}
		h, path, f, records := askSetup(t, r, limiterF{errF: errors.New("the counter is down")})
		f.signedIn()
		f.allow = true

		res := askOnce(t, h, path, asked)
		if res.Code != http.StatusServiceUnavailable || len(r.told) != 0 || len(*records) != 0 {
			t.Errorf("an ask during a counting outage = %d, %d notices, %d records; want 503 and nothing written: %s",
				res.Code, len(r.told), len(*records), res.Body)
		}
	})

	t.Run("a spent limit writes nothing", func(t *testing.T) {
		r := &reachF{recipients: []uuid.UUID{other}}
		h, path, f, records := askSetup(t, r, limiterF{allowed: false})
		f.signedIn()
		f.allow = true

		res := askOnce(t, h, path, asked)
		if res.Code != http.StatusTooManyRequests || len(r.told) != 0 || len(*records) != 0 {
			t.Errorf("an ask past the limit = %d, %d notices, %d records; want 429 and nothing written: %s",
				res.Code, len(r.told), len(*records), res.Body)
		}
	})

	t.Run("a composition that wired no reach mounts no door that lies", func(t *testing.T) {
		// Ask with no door answers the outage sentence rather than pretending the
		// ask went somewhere. CanAsk is what the refusal page reads to draw no
		// button at all; this is the same fact from the other side.
		r := &reachF{recipients: []uuid.UUID{other}}
		_, app := dbtest.Schema(t)
		f := &fixture{
			tenant: tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"},
			app:    app,
			logs:   &lines{},
		}
		api, router := httpx.New(httpx.Options{
			Installation: host, PublicHost: host, Tenants: f, Conn: app,
			Authorize: f, Entitle: f, Authenticate: f.authenticate,
			Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
		api.Declare([]tenancy.Grant{{Permission: "widget:read", Label: "read widgets"}})
		httpx.Register(api.Surfaces(probe).App, huma.Operation{
			OperationID: "ask-without-a-door", Method: http.MethodPost, Path: "/asks",
			DefaultStatus: http.StatusAccepted,
		}, httpx.SignedIn(), func(ctx context.Context, in *askInput) (*struct{}, error) {
			return nil, httpx.Ask(ctx, httpx.AccessAsk{Permission: in.Body.Permission})
		})
		f.signedIn()
		f.allow = true

		res := askOnce(t, router, at(api, "/asks"), asked)
		if res.Code != http.StatusServiceUnavailable || len(r.told) != 0 {
			t.Errorf("an ask with no reach wired = %d and %d notices written; want 503 and nothing: %s",
				res.Code, len(r.told), res.Body)
		}
	})
}

// TestTheAskRefusesACallerWhoIsNobody is the door's own guard: the command names
// the asker from the session, so a request that reached it without one is refused
// rather than recorded against nobody.
func TestTheAskRefusesACallerWhoIsNobody(t *testing.T) {
	r := &reachF{recipients: []uuid.UUID{uuid.New()}}
	h, path, _, records := askSetup(t, r, limiterF{allowed: true})

	// No cookie, so no principal: SignedIn answers first.
	req := httptest.NewRequest(http.MethodPost, "http://"+host+path,
		strings.NewReader(`{"permission":"widget:read"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("an anonymous ask = %d %s, want the refusal the guard gives", w.Code, w.Body)
	}
	if len(r.told) != 0 || len(*records) != 0 {
		t.Errorf("an anonymous ask wrote %d notices and %d records", len(r.told), len(*records))
	}
}
