package httpx

// public_write_burst_internal_test.go is the kernel's copy of the client case
// that CI could not pass: 75 anonymous submissions at one public door, released
// together, with the counter's row held by somebody else so the queue is real.
// The client's own version lives in an application that cannot read the figures
// it is asserting on; this one is in the package that owns them, so
// publicWriteLimit and publicWriteWindow are read rather than restated, and the
// counter underneath is the one every deployment runs.
//
// It is written here as well as in package httpx_test because the two fail for
// different reasons. The conformance file pins what the door does with each of
// kit/limit's four answers; this one pins that the answers and the door agree
// under a flood, which is the thing that broke.

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
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
	"github.com/septagon-oss/platformkit/kit/limit"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

const (
	// burstKnocks is the client's figure: its flagship case fires 75 goroutines at
	// one review door. publicWriteLimit is this package's own, read below — and it
	// is below the number of knocks, which is what makes a run where nothing
	// queued fail this case rather than pass it empty.
	burstKnocks = 75
	burstHost   = "burst.test"
)

// burstAnswer is what the door's handler gives back: a handler returning a
// struct with no fields is answered 204 by huma, and this case counts the 200s a
// submission that got through is given.
type burstAnswer struct {
	Body struct {
		Said string `json:"said"`
	}
}

var burstTenant = tenancy.Tenant{ID: uuid.MustParse("11111111-1111-4111-8111-111111111111"), Slug: "acme", Name: "Acme"}

// held is a connection held open-handed, the way kit/app holds its own: read
// whatever the context says, because at this point in the chain the context has
// nothing to say.
func held(c *db.Conn) limit.Connections {
	return func(context.Context) (*db.Conn, bool) { return c, true }
}

// burstTenants resolves the one host this door is asked at.
type burstTenants struct{}

func (burstTenants) ByHost(context.Context, db.Tx[db.System], string) (tenancy.Tenant, error) {
	return burstTenant, nil
}

// TestAPublicWriteBurstAtOneDoorStopsAtTheWindow is the acceptance sentence told
// at the door: at most the allowance is admitted, and the rest are 429s.
func TestAPublicWriteBurstAtOneDoorStopsAtTheWindow(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	reached := &atomic.Int64{}
	api, router := New(Options{
		Cache:      cache.Memory("pkit"),
		PublicHost: burstHost, Installation: burstHost, Conn: conn,
		Tenants:   burstTenants{},
		Authorize: nothing{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{}, false, nil
		},
		// The counter is handed the pool rather than told to find it on the request
		// context, because this middleware runs ahead of the one that puts a
		// connection there (a.publicWrites comes before a.transaction, and
		// WithConn happens inside that). It is how the composition does it:
		// `WriteLimiter: limit.Postgres(a.held.read)` in kit/app/app.go.
		WriteLimiter: limit.Postgres(held(conn)),
		Log:          slog.New(slog.DiscardHandler),
	})
	Register(api.Surfaces("burst").Public, huma.Operation{
		OperationID: "burst-ask", Method: http.MethodPost, Path: "/ask",
	}, Public(), func(context.Context, *struct{}) (*burstAnswer, error) {
		reached.Add(1)
		return &burstAnswer{}, nil
	})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the fixture does not describe itself: %v", err)
	}

	door := api.Surfaces("burst").Public.Path("/ask")
	knock := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "http://"+burstHost+door, strings.NewReader(`{"answer":"yes"}`))
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}

	// The counter is a fixed window somebody else may have spent: open the window
	// first, so what is counted below is this burst. The knock that opens it is
	// counted, because it is a submission at the same door.
	var admitted atomic.Int64
	switch first := knock(); first.Code {
	case http.StatusOK:
		admitted.Add(1)
	case http.StatusTooManyRequests:
		t.Fatal("the door refused the knock that was meant to open the window")
	default:
		t.Fatalf("the door answered the first submission %d, want it to be the handler's", first.Code)
	}

	// The store is slowed by the real mechanism: another session holds the row this
	// door counts under — read back rather than reconstructed, because the key is
	// this middleware's own shape — and the burst queues behind it the way the
	// client's did behind a loaded runner.
	dbtest.Hold(t, admin, 20*time.Second,
		"UPDATE platformkit_limits SET count = count WHERE key = $1", storedKey(t, admin))

	start := make(chan struct{})
	var wg sync.WaitGroup
	var refused, waitedTheWindow, other atomic.Int64
	for range burstKnocks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			res := knock()
			switch res.Code {
			case http.StatusTooManyRequests:
				refused.Add(1)
				// The whole window in Retry-After is only ever the queued refusal: a
				// window that was spent carries what is left of it, which is less.
				if res.Header().Get("Retry-After") == strconv.Itoa(int(publicWriteWindow.Seconds())) {
					waitedTheWindow.Add(1)
				}
			case http.StatusOK:
				admitted.Add(1)
			default:
				other.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := other.Load(); got != 0 {
		t.Errorf("%d submissions were answered with neither the handler nor a 429: the door is meant to "+
			"answer one or the other, and a 500 out of a limiter is an outage it invented", got)
	}
	if got := reached.Load(); got > int64(publicWriteLimit) {
		t.Errorf("%d submissions reached the handler at a limit of %d, which is the client's failure: a burst "+
			"that waits behind its own counter is admitted by every caller that fails open on an error",
			got, publicWriteLimit)
	}
	if got := admitted.Load(); got > int64(publicWriteLimit) {
		t.Errorf("%d submissions were answered 200 at a limit of %d", got, publicWriteLimit)
	}
	if refused.Load() == 0 {
		t.Errorf("none of the %d submissions was refused; the queue never formed, so this case measured nothing", burstKnocks)
	}
	// And the submissions that were refused for waiting say so honestly, rather
	// than with a window they never read: Retry-After is the whole window, which is
	// the one figure that cannot understate what is left of a row nobody saw.
	if waitedTheWindow.Load() == 0 {
		t.Errorf("no refusal carried the whole window of %s in Retry-After, so none of them was the queued "+
			"refusal this case is about: the queue was answered some other way", publicWriteWindow)
	}
}

// storedKey is the counter the door wrote, read back rather than reconstructed:
// the key is the middleware's own, and a test that spelled it would be a second
// opinion about what two customers' counters are made of.
func storedKey(t *testing.T, admin *sql.DB) string {
	t.Helper()
	rows, err := admin.QueryContext(t.Context(), "SELECT key FROM platformkit_limits ORDER BY key")
	if err != nil {
		t.Fatalf("read the stored keys: %v", err)
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatalf("read a stored key: %v", err)
		}
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the stored keys: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("the burst wrote %d counters (%v), want the one this door counts under", len(keys), keys)
	}
	return keys[0]
}
