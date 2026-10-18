package httpx_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// heldLookups is a fixture of two tenants, each answering at one host, whose
// loader reads the tenant's default language from its row, counts the lookups
// per host, and holds the first lookup of the held host open until released.
type heldLookups struct {
	api      *httpx.API
	router   http.Handler
	surface  httpx.Surfaces
	ids      map[string]uuid.UUID
	loads    map[string]*atomic.Int32
	entered  chan struct{}
	release  func()
	ctx      context.Context
	released chan struct{}
}

const otherHost = "other.test"

func newHeldLookups(t *testing.T, held string) *heldLookups {
	t.Helper()
	owner, conn := dbtest.Schema(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	f := &heldLookups{
		ids:      map[string]uuid.UUID{host: uuid.New(), otherHost: uuid.New()},
		loads:    map[string]*atomic.Int32{host: new(atomic.Int32), otherHost: new(atomic.Int32)},
		entered:  make(chan struct{}),
		released: make(chan struct{}),
		ctx:      ctx,
	}
	f.release = sync.OnceFunc(func() { close(f.released) })
	t.Cleanup(f.release)
	for h, id := range f.ids {
		slug := strings.TrimSuffix(h, ".test")
		if _, err := owner.ExecContext(ctx,
			"INSERT INTO tenants (id, slug, name) VALUES ($1, $2, $2)", id, slug); err != nil {
			t.Fatal(err)
		}
	}
	var holding atomic.Bool
	f.api, f.router = httpx.New(httpx.Options{
		Conn: conn, Installation: "installation.test", PublicHost: host,
		Cache: cache.Memory("pkit"),
		Tenants: loaderFunc(func(ctx context.Context, tx db.Tx[db.System], h string) (tenancy.Tenant, error) {
			id, ok := f.ids[h]
			if !ok {
				return tenancy.Tenant{}, tenancy.ErrNoSuchHost
			}
			f.loads[h].Add(1)
			var tag string
			if err := tx.DB().Raw("SELECT default_locale FROM tenants WHERE id = ?", id).Row().Scan(&tag); err != nil {
				return tenancy.Tenant{}, err
			}
			if h == held && holding.CompareAndSwap(false, true) {
				close(f.entered)
				select {
				case <-f.released:
				case <-ctx.Done():
					return tenancy.Tenant{}, ctx.Err()
				}
			}
			return tenancy.Tenant{ID: id, Slug: h, Languages: &tenancy.Languages{Default: tag}}, nil
		}),
		Authorize: authorizerFunc(func(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
			return true, nil
		}),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New()}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	f.surface = f.api.Surfaces("tenant")
	httpx.Register(f.surface.App, huma.Operation{
		OperationID: "tenant-current-language", Method: http.MethodGet, Path: "/current",
	}, httpx.SignedIn(), func(ctx context.Context, _ *struct{}) (*body, error) {
		tenant, _ := tenancy.FromContext(ctx)
		out := new(body)
		out.Body.Tenant = tenant.Languages.Default
		return out, nil
	})
	httpx.Register(f.surface.App, huma.Operation{
		OperationID: "tenant-replace-languages", Method: http.MethodPut, Path: "/languages",
	}, httpx.SignedIn(), func(ctx context.Context, _ *struct{}) (*body, error) {
		tx, ok := httpx.TxFrom(ctx)
		if !ok {
			return nil, fmt.Errorf("no tenant transaction")
		}
		tenant, _ := tenancy.FromContext(ctx)
		if err := tx.DB().Exec("UPDATE tenants SET default_locale = 'pt-PT', locale_revision = locale_revision + 1 WHERE id = ?", tenant.ID).Error; err != nil {
			return nil, err
		}
		httpx.ForgetTenantOnCommit(ctx, tenant.ID)
		return new(body), nil
	})
	return f
}

// read asks one host for the language its tenant resolved to.
func (f *heldLookups) read(h string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "http://"+h+f.surface.App.Path("/current"), nil).WithContext(f.ctx)
	req.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}

func (f *heldLookups) waitEntered(t *testing.T) {
	t.Helper()
	select {
	case <-f.entered:
	case <-f.ctx.Done():
		t.Fatal("the held lookup never started")
	}
}

// TestConcurrentColdRequestsForOneHostShareOneLookup: keying the shared lookup by
// drop generation keeps the promise it exists for — with no drop, every request
// that arrives while one host's lookup is outstanding is answered by that lookup.
func TestConcurrentColdRequestsForOneHostShareOneLookup(t *testing.T) {
	f := newHeldLookups(t, host)
	const callers = 8
	answers := make(chan *httptest.ResponseRecorder, callers)
	go func() { answers <- f.read(host) }()
	f.waitEntered(t)
	for range callers - 1 {
		go func() { answers <- f.read(host) }()
	}
	// The joiners have no signal of their own to wait on; give them time to
	// arrive at the outstanding lookup before it answers.
	time.Sleep(time.Second)
	f.release()
	for range callers {
		select {
		case w := <-answers:
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"tenant":"en"`) {
				t.Errorf("a caller of the shared lookup = %d: %s", w.Code, w.Body.String())
			}
		case <-f.ctx.Done():
			t.Fatal("a caller of the shared lookup never answered")
		}
	}
	if n := f.loads[host].Load(); n != 1 {
		t.Errorf("%d lookups for %d concurrent cold requests to one host, want 1", n, callers)
	}
	if w := f.read(host); w.Code != http.StatusOK {
		t.Fatalf("a warm request = %d: %s", w.Code, w.Body.String())
	}
	if n := f.loads[host].Load(); n != 1 {
		t.Errorf("%d lookups after a warm request, want 1: the shared answer was not remembered", n)
	}
}

// TestAnotherTenantsLanguageCommitCostsALookupOneQueryAndNoWrongAnswer: the drop
// count is one for the whole cache, so another tenant's committed change while a
// lookup is outstanding may keep that lookup out of the cache. The request it
// served and the next one are still answered from their own tenant's row, and at
// most one more query is all it costs: the request after that is answered from
// the cache.
func TestAnotherTenantsLanguageCommitCostsALookupOneQueryAndNoWrongAnswer(t *testing.T) {
	f := newHeldLookups(t, host)
	if w := f.read(otherHost); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"tenant":"en"`) {
		t.Fatalf("warm the other tenant's host = %d: %s", w.Code, w.Body.String())
	}
	first := make(chan *httptest.ResponseRecorder, 1)
	go func() { first <- f.read(host) }()
	f.waitEntered(t)
	if w := request(t, f.router, http.MethodPut, f.surface.App.Path("/languages"), otherHost); w.Code != http.StatusOK {
		t.Fatalf("the other tenant's language change = %d: %s", w.Code, w.Body.String())
	}
	if w := f.read(otherHost); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"tenant":"pt-PT"`) {
		t.Errorf("the other tenant after its commit = %d: %s; want pt-PT", w.Code, w.Body.String())
	}
	f.release()
	select {
	case w := <-first:
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"tenant":"en"`) {
			t.Errorf("the request the outstanding lookup served = %d: %s; want en", w.Code, w.Body.String())
		}
	case <-f.ctx.Done():
		t.Fatal("the outstanding lookup never answered")
	}
	var settled int32
	for i := range 2 {
		w := f.read(host)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"tenant":"en"`) {
			t.Errorf("request %d after the other tenant's commit = %d: %s; want en", i+1, w.Code, w.Body.String())
		}
		n := f.loads[host].Load()
		switch {
		case n > 2:
			t.Errorf("after request %d: %d lookups of the unchanged tenant's host, want at most 2", i+1, n)
		case i == 0:
			settled = n
		case n != settled:
			t.Errorf("after request %d: %d lookups of the unchanged tenant's host, want %d: the cache never settled", i+1, n, settled)
		}
	}
}
