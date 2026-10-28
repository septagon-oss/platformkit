package main

// A refusal to send an invitation again is the one refusal in this application that
// states its wait outside the kernel's own rendering: the module writes the header
// itself, from the instant its decision computed, because the fence is the
// invitation's own cooldown rather than a counted door. Every shut door that goes
// through kit/httpx answers from one helper which floors the wait at a whole second
// and rounds it up; this header answers from a time.Time the row handed back, and the
// figure it owes is the same one — the instant the fence closes, stated in whole
// seconds of UTC. Stated down, a caller is back at the fence before it has closed and
// is refused again; stated in the zone a driver may hand back rather than UTC, the
// digits are that zone's wall clock wearing a "GMT" suffix, and the wait is off by the
// whole offset either way round. So the wait is refused here in both directions.

import (
	"net/http"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// statedInstant finds the instant a cooldown refusal names in its own body, which is
// the first moment a replacement may be requested.
var statedInstant = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})`)

// oneStatement is how far apart two whole-second renderings of one instant may sit.
// A `Retry-After` states an instant in seconds, so a header that is honest about the
// instant the body promises differs from it by less than the second it rounds away
// and the second a ceiling may add.
const oneStatement = 2 * time.Second

// retryAfter reads a `Retry-After` the way RFC 9110 lets a caller read it — a delay in
// whole seconds from the answer, or the HTTP-date of the instant it names — as the
// instant it asks the caller to sit out until.
func retryAfter(t *testing.T, header string, asked time.Time) time.Time {
	t.Helper()
	if seconds, err := strconv.ParseInt(header, 10, 64); err == nil {
		return asked.Add(time.Duration(seconds) * time.Second)
	}
	at, err := http.ParseTime(header)
	if err != nil {
		t.Fatalf("Retry-After = %q is neither a delay in whole seconds nor an HTTP-date, so no caller can honour it: %v", header, err)
	}
	return at
}

func TestARefusedResendAsksAWaitTheCallerCanHonour(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	const endpoint = "/api/v1/user/invitations"
	code, invited, _ := invitationHTTP(t, cfg, admin, endpoint,
		`{"email":"cooling@acme.localhost","displayName":"Cooling","roles":["member"]}`, "cooling.invite")
	if code != http.StatusCreated {
		t.Fatalf("the invitation = %d %s, want 201: the refusal's wait is only worth reading under a pending invitation", code, invited)
	}
	id := field(t, invited, "id")

	asked := time.Now().UTC()
	code, refusal, headers := invitationHTTP(t, cfg, admin, endpoint+"/"+id+"/resend",
		`{"expectedRevision":1}`, "cooling.resend")
	if code != http.StatusTooManyRequests {
		t.Fatalf("a resend inside the cooldown = %d %s, want 429: whether the fence held is this case's own reachability, not its subject", code, refusal)
	}

	named := statedInstant.FindString(refusal)
	if named == "" {
		t.Fatalf("the cooldown refusal states no instant a replacement may be requested at: %s", refusal)
	}
	promise, err := time.Parse(time.RFC3339, named)
	if err != nil {
		t.Fatalf("the instant the refusal states, %q, does not parse as RFC 3339: %v", named, err)
	}
	if !promise.After(asked) {
		t.Errorf("the refusal at %s names %s as the first moment a replacement may be requested, which the caller has already reached",
			asked.Format(time.RFC3339), named)
	}

	header := headers.Get("Retry-After")
	if header == "" {
		t.Fatal("a 429 arrived with no Retry-After: a refusal that names no wait asks to be retried now")
	}
	// A date written in a zone other than UTC keeps that zone's digits and adds the
	// literal "GMT", so the instant read off it is that zone's whole offset away from
	// the one the decision computed. The two figures bound the drift from both sides:
	// short, and the caller is back at the fence before it has closed; long, and it
	// sits out a wait nobody decided, on the answer of a header it was told to trust.
	said := retryAfter(t, header, asked)
	if !said.After(asked) {
		t.Errorf("Retry-After = %q names %s, no later than the %s the response itself arrived: the wait of a cooldown refusal is the rest of the cooldown, never nothing",
			header, said.Format(time.RFC3339), asked.Format(time.RFC3339))
	}
	if drift := said.Sub(promise); drift < 0 || drift >= oneStatement {
		t.Errorf("Retry-After = %q names %s, %s away from the %s the same refusal states in its body: the wait is that instant stated in whole seconds of UTC — never down, which asks the caller back before the fence has closed, and never a zone's offset away, which is a wait nobody decided",
			header, said.Format(time.RFC3339), drift, promise.Format(time.RFC3339))
	}
}
