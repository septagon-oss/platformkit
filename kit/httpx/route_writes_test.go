package httpx_test

// route_writes_test.go pins the counted-write arm of this kernel's two write
// limits: the one that runs on an authenticated route, after the permission guard
// and before a byte of the body is read. public_write_queued_test.go pins the
// anonymous arm and its address key; this file is the person-shaped half.

import (
	"context"
	"errors"
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

// recordedLimit is the counter this file hands the arm: it answers from fields a
// case sets, and keeps every key it was asked about, which is the only way to see
// what a key is made of. kit/limit's own cases make the store produce its four
// outcomes; what is decided here is the door's reading of one of them.
type recordedLimit struct {
	mu     sync.Mutex
	keys   []string
	ok     bool
	retry  time.Duration
	err    error
	called atomic.Int64
}

func (l *recordedLimit) Allow(_ context.Context, key string, _ int, _ time.Duration) (bool, time.Duration, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.keys = append(l.keys, key)
	l.called.Add(1)
	return l.ok, l.retry, l.err
}

func (l *recordedLimit) asked() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.keys...)
}

// countedDoor composes the one authenticated POST this file needs, counted per
// actor and per tenant, and counts the requests that reached the handler. It
// answers the boot validation rather than dying on it, so the case that wants a
// refusal can ask for one: t.Fatalf would end the test that is trying to assert it.
func countedDoor(t *testing.T, l httpx.WriteLimiter, limits ...httpx.WriteLimit) (router http.Handler, door string, reached *atomic.Int64, who uuid.UUID) {
	t.Helper()
	router, door, reached, who, err := countedDoorErr(t, l, false, limits...)
	if err != nil {
		t.Fatalf("the fixture does not describe itself: %v", err)
	}
	return router, door, reached, who
}

// countedDoorErr is that fixture with its boot answer left to the caller, and
// with the sign-in the case is about: an anonymous caller is the other arm's
// subject, and this arm has to be seen to know that.
// When anonymous is true the door is mounted where an anonymous caller actually
// stands — the Public surface — because that is the only place this arm meets a
// request with no principal, and an App route refuses one before a counter is
// ever asked.
func countedDoorErr(t *testing.T, l httpx.WriteLimiter, anonymous bool, limits ...httpx.WriteLimit) (http.Handler, string, *atomic.Int64, uuid.UUID, error) {
	t.Helper()
	who := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	_, conn := dbtest.Schema(t)
	reached := &atomic.Int64{}
	api, router := httpx.New(httpx.Options{
		Cache:      cache.Memory("pkit"),
		PublicHost: host, Installation: installationHost, Conn: conn,
		Tenants: loaderFunc(func(context.Context, db.Tx[db.System], string) (tenancy.Tenant, error) {
			return tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}, nil
		}),
		Authorize: authorizerFunc(func(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
			return true, nil
		}),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			if anonymous {
				return tenancy.Principal{}, false, nil
			}
			return tenancy.Principal{UserID: who}, true, nil
		},
		WriteLimiter: l,
		Log:          slog.New(slog.DiscardHandler),
	})
	if len(limits) == 0 {
		limits = []httpx.WriteLimit{
			{Per: httpx.LimitPerActor, Limit: 2, Window: "1m"},
			{Per: httpx.LimitPerTenant, Limit: 10, Window: "1m"},
		}
	}
	key, value := httpx.WriteLimits(limits...)
	where, guard := api.Surfaces("counted").App, httpx.Permission("file:manage")
	if anonymous {
		where, guard = api.Surfaces("counted").Public, httpx.Public()
	}
	httpx.Register(where, huma.Operation{
		OperationID: "counted-ask", Method: http.MethodPost, Path: "/upload",
		Extensions: map[string]any{key: value},
	}, guard, func(context.Context, *struct{}) (*body, error) {
		reached.Add(1)
		return &body{}, nil
	})
	if err := api.ValidateDeclarations(); err != nil {
		return nil, "", reached, who, err
	}
	return router, where.Path("/upload"), reached, who, nil
}

// submit posts the body the counted route would read.
func submit(t *testing.T, router http.Handler, door string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "http://"+host+door, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	// A credential the authenticate middleware will look at: without one the
	// request never reaches Options.Authenticate at all and answers as the
	// anonymous request it is. Whether that caller is anybody is the case's
	// business, and the fixture's Authenticate hook is what answers it.
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	return res
}

// TestACountedRouteRefusesTheSpentCaller is the door's side of the two answers
// that matter: a spent window is a 429 carrying how long to wait, with nothing
// handed to the handler, and a counter that cannot answer is the counter's outage,
// which the route fails open on rather than turning into a denial.
func TestACountedRouteRefusesTheSpentCaller(t *testing.T) {
	t.Run("a spent window refuses with Retry-After and no write", func(t *testing.T) {
		limit := &recordedLimit{ok: false, retry: 3 * time.Second}
		router, door, reached, _ := countedDoor(t, limit)
		res := submit(t, router, door)
		if res.Code != http.StatusTooManyRequests {
			t.Errorf("a counted write past its window = %d, want 429: %s", res.Code, res.Body)
		}
		if got := res.Header().Get("Retry-After"); got != "3" {
			t.Errorf("Retry-After is %q, want the counter's own 3 seconds", got)
		}
		if reached.Load() != 0 {
			t.Errorf("the refused write reached the handler %d times, want never", reached.Load())
		}
	})
	t.Run("a counter that cannot answer lets the write through", func(t *testing.T) {
		limit := &recordedLimit{ok: false, err: errors.New("the counter is down")}
		router, door, reached, _ := countedDoor(t, limit)
		res := submit(t, router, door)
		if res.Code != http.StatusOK || reached.Load() != 1 {
			t.Errorf("a limiter outage answered %d having reached the handler %d times, want 200 once",
				res.Code, reached.Load())
		}
	})
	t.Run("the actor's own bound spends nothing of the tenant's", func(t *testing.T) {
		// The actor entry is first and is spent, so the tenant bucket is never
		// asked: a caller already over their own allowance may not also drain the
		// shared one while they wait.
		limit := &recordedLimit{ok: false, retry: time.Minute}
		router, door, _, _ := countedDoor(t, limit)
		submit(t, router, door)
		asked := limit.asked()
		if len(asked) != 1 {
			t.Fatalf("the door asked %d counters (%v), want the actor's alone", len(asked), asked)
		}
		if !strings.Contains(asked[0], "actor") {
			t.Errorf("the counter asked is %q, want the actor's", asked[0])
		}
	})
}

// TestTheCountedKeysNameTheCallerAndTheMountedRoute checks the two things a key
// has to get right: it distinguishes who is asking without naming an address an
// office proxy shares, and it names the mounted pattern rather than the path, or
// anybody who can type a URL can inflate the table.
func TestTheCountedKeysNameTheCallerAndTheMountedRoute(t *testing.T) {
	limit := &recordedLimit{ok: true}
	router, door, reached, who := countedDoor(t, limit)
	if res := submit(t, router, door); res.Code != http.StatusOK || reached.Load() != 1 {
		t.Fatalf("an admitted counted write = %d, %d reached; want 200 once", res.Code, reached.Load())
	}
	asked := limit.asked()
	if len(asked) != 2 {
		t.Fatalf("the door asked %d counters (%v), want the actor's and the tenant's", len(asked), asked)
	}
	if !strings.Contains(asked[0], who.String()) {
		t.Errorf("the actor's key %q does not name the caller %s", asked[0], who)
	}
	if !strings.HasSuffix(asked[1], " tenant") {
		t.Errorf("the tenant's key %q is not the shared one", asked[1])
	}
	for _, k := range asked {
		// The door variable is the mounted path with no value in it; posting to a
		// path with a value in it is the other half of this assertion, and the
		// route is the same answer either way.
		if !strings.Contains(k, door) {
			t.Errorf("the key %q names something other than the mounted route %s", k, door)
		}
	}
}

// TestAnAnonymousCallerIsNotCountedAgainstNobody keeps the actor entry from
// becoming the thing these two arms were split over: with no principal there is
// nobody to count, and a bucket named after the nil uuid would gather every
// stranger into one counter, which is the address-shaped mistake the Public arm
// owns instead. The tenant entry is still asked, because a tenant's fuse burns
// whoever lit it, and a spent one is still refused.
func TestAnAnonymousCallerIsNotCountedAgainstNobody(t *testing.T) {
	// The anonymous door is the Public surface's, so its own anonymous counter
	// runs first; everything under "actor" below is this arm's answer.
	limit := &recordedLimit{ok: false, retry: time.Minute}
	router, door, reached, _, err := countedDoorErr(t, limit, true)
	if err != nil {
		t.Fatalf("the fixture does not describe itself: %v", err)
	}
	limit.ok = true
	if res := submit(t, router, door); res.Code != http.StatusOK || reached.Load() != 1 {
		t.Fatalf("an anonymous counted write inside the window = %d with %d at the handler, want 200 once",
			res.Code, reached.Load())
	}
	asked := limit.asked()
	var mine []string
	for _, k := range asked {
		if strings.Contains(k, "actor") {
			t.Errorf("a caller with nobody in them spent an actor counter: %q", k)
		}
		if strings.HasSuffix(k, " tenant") {
			mine = append(mine, k)
		}
	}
	if len(mine) != 1 {
		t.Errorf("the counted-write arm asked %v, want its tenant bucket once among them", asked)
	}
}

// TestAnOperationThatCountsItsWritesBadlyRefusesToStart is the boot gate: a limit
// nobody can read is worse than no limit, so a ceiling that is not a count, a
// window that is not a duration, or an identity that is neither of the two stops
// the composition rather than counting nothing quietly.
func TestAnOperationThatCountsItsWritesBadlyRefusesToStart(t *testing.T) {
	for _, one := range []struct {
		name  string
		limit httpx.WriteLimit
		wants string
	}{
		{"an address is the other arm's subject", httpx.WriteLimit{Per: "ip", Limit: 5, Window: "1m"}, "ip"},
		{"a ceiling of zero counts nothing", httpx.WriteLimit{Per: httpx.LimitPerActor, Limit: 0, Window: "1m"}, "0 submissions"},
		{"a window nobody can read", httpx.WriteLimit{Per: httpx.LimitPerActor, Limit: 5, Window: "1min"}, "1min"},
	} {
		_, _, _, _, err := countedDoorErr(t, &recordedLimit{ok: true}, true, one.limit)
		if err == nil {
			t.Errorf("%s: the operation started, want it refused at boot", one.name)
			continue
		}
		if !strings.Contains(err.Error(), one.wants) {
			t.Errorf("%s: boot said %v, want it to name %q", one.name, err, one.wants)
		}
	}
}
