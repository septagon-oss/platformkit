package main

// A refused resend answers its wait in three places: the `Retry-After` a caller is told
// to honour, the sentence the refusal carries, and the redacted reading's `nextResendAt`,
// which is the figure a pending badge and a rescind action read. The first two come from
// the instant the command's own decision computed; the third is projected from the row by
// the reading — `requestedAt` plus the composed resend interval. That is one rule written
// twice, so a caller obeying the header and a screen obeying the reading can be told two
// different waits, and the interval itself can answer the invitation's lifetime instead.
// This case compares all three: the same instant, and the interval the application
// composes rather than the link's lifetime.
//
// The instant arrives from the driver in the zone the serving process runs in, not in
// UTC: `db.Now` truncates to what the column keeps but the read-back carries that zone,
// so a header formatted straight from it states that zone's wall clock under a literal
// "GMT" and the answer is the zone's whole offset away. The row's own answer and the
// refusal's own sentence carry the offset and stay honest; only the header loses it.
// Run under TZ=UTC the two agree by accident of where the process runs.

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// resendWait is the part of the redacted reading that answers the wait.
type resendWait struct {
	State        string     `json:"state"`
	Revision     int64      `json:"revision"`
	RequestedAt  *time.Time `json:"requestedAt"`
	CanResend    bool       `json:"canResend"`
	NextResendAt *time.Time `json:"nextResendAt"`
}

func TestTheRefusedResendsWaitAndTheReadingPromiseOneInstant(t *testing.T) {
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
		`{"email":"agreeing@acme.localhost","displayName":"Agreeing","roles":["member"]}`, "agreeing.invite")
	if code != http.StatusCreated {
		t.Fatalf("the invitation = %d %s, want 201: the wait is only worth comparing under a pending invitation", code, invited)
	}
	id := field(t, invited, "id")

	asked := time.Now().UTC()
	code, refusal, headers := invitationHTTP(t, cfg, admin, endpoint+"/"+id+"/resend",
		`{"expectedRevision":1}`, "agreeing.resend")
	if code != http.StatusTooManyRequests {
		t.Fatalf("a resend inside the cooldown = %d %s, want 429: whether the fence held is this case's reachability, not its subject", code, refusal)
	}
	header := headers.Get("Retry-After")
	if header == "" {
		t.Fatal("a 429 arrived with no Retry-After: a refusal that names no wait asks to be retried now")
	}
	named := statedInstant.FindString(refusal)
	if named == "" {
		t.Fatalf("the cooldown refusal states no instant a replacement may be requested at: %s", refusal)
	}
	sentence, err := time.Parse(time.RFC3339, named)
	if err != nil {
		t.Fatalf("the instant the refusal states, %q, does not parse as RFC 3339: %v", named, err)
	}

	code, reading := do(t, cfg, admin, http.MethodGet, acmeHost, "/api/v1/auth/invitations/"+id, "")
	if code != http.StatusOK {
		t.Fatalf("the redacted reading = %d %s, want 200: the wait a screen reads is this row's answer", code, reading)
	}
	var row resendWait
	if err := json.Unmarshal([]byte(reading), &row); err != nil {
		t.Fatalf("the redacted reading is not the reading this application promises: %v\n%s", err, reading)
	}
	if row.State != "pending" {
		t.Errorf("the reading of the invitation the refusal refused = %q, want pending", row.State)
	}
	if row.Revision != 1 {
		t.Errorf("the reading after the refused resend is at revision %d, want 1: a refusal that moved the row would leave the caller's expected revision stale", row.Revision)
	}
	if row.CanResend {
		t.Errorf("the reading answers canResend true while the refusal of the same request says a replacement waits until %s", named)
	}
	if row.NextResendAt == nil || row.RequestedAt == nil {
		t.Fatalf("the reading of a pending invitation names no requestedAt or nextResendAt: %s", reading)
	}

	// Three answers, one instant. The header is read the way a caller reads it, so it is
	// compared both ways: a header ahead of the row's promise is a wait nobody decided, and
	// one behind it asks the caller back before the fence has closed.
	said := retryAfter(t, header, asked)
	if drift := said.Sub(*row.NextResendAt); drift < -oneStatement || drift >= oneStatement {
		t.Errorf("Retry-After = %q names %s, %s away from the reading's nextResendAt %s: the refusal's wait and the reading's wait are one rule and must name one instant",
			header, said.Format(time.RFC3339), drift, row.NextResendAt.Format(time.RFC3339))
	}
	if drift := sentence.Sub(*row.NextResendAt); drift < -oneStatement || drift >= oneStatement {
		t.Errorf("the refusal states %s in its own body, %s away from the reading's nextResendAt %s",
			named, drift, row.NextResendAt.Format(time.RFC3339))
	}
	// And the fence is the resend interval the application composes, never the link's
	// lifetime: an hour of waiting on a five-minute cooldown is a wait nobody decided.
	if wait := row.NextResendAt.Sub(*row.RequestedAt); wait != authcontracts.ResetInterval {
		t.Errorf("the reading fences a replacement for %s from the request, want the composed resend interval %s, not the link's %s",
			wait, authcontracts.ResetInterval, authcontracts.TokenLifetime)
	}
}
