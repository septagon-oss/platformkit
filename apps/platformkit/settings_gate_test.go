package main

// The switch in front of the settings door is read from somewhere, and "somewhere"
// can fail to answer. kit/flags' rule for a failed evaluation is to hand the caller
// the fallback the caller passed — false, "change control is off, write away" — so a
// provider that is unreachable, unauthorised or misconfigured reads as a switch that
// is off, and the tenant's settings change hands with no second account anywhere near
// them. Decision 0010 calls that answer a silent allow.
//
// The door answers instead. The three cases below are the whole of what it may say:
// it cannot ask (refused, with the status that asks to be retried and the address that
// works meanwhile), it asks and the switch is off (the write goes), it asks and the
// switch is on (refused, with the way through named). The reference application ships
// the only evaluator this tree composes, configFlags, which cannot fail — so these
// call the gate directly, and the day a real provider sits behind it this is the case
// that says what it must do when the provider has a bad day.

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/flags"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	changecontracts "github.com/septagon-oss/platformkit/modules/change/contracts"
)

// offlineFlags is an evaluator with nothing to say: the provider unreachable, the
// token expired, the answer unreadable. kit/flags hands every one of those back as an
// error and no decision, which is the situation under test.
type offlineFlags struct{}

var _ flags.Evaluator = offlineFlags{}

func (offlineFlags) Boolean(context.Context, string, flags.Subject, bool) (flags.Decision, error) {
	return flags.Decision{}, errors.New("the flag service is not answering")
}

func TestTheSettingsDoorAnswersForWhatItCannotAsk(t *testing.T) {
	_, conn := dbtest.Schema(t)
	ask := func(eval flags.Evaluator) error {
		return db.Run(tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: uuid.New(), Slug: "acme"}),
			conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				return settingsGate{eval: eval}.Check(ctx, tx)
			})
	}

	unreadable := ask(offlineFlags{})
	var outage *problem.Problem
	if unreadable == nil || !errors.As(unreadable, &outage) || outage.Status != http.StatusServiceUnavailable {
		t.Fatalf("a switch that could not be read answered %v; the write is refused with a status, "+
			"not allowed because the question went unanswered", unreadable)
	}
	if !strings.Contains(outage.Detail, proposalAddress) || !strings.Contains(outage.Detail, siteSettingsFlag) {
		t.Errorf("the refusal for an unreadable switch says %q; it names %s as the door that answers "+
			"while the provider is down, and %s as the switch that did not answer", outage.Detail,
			proposalAddress, siteSettingsFlag)
	}
	// A fresh Problem per refusal: kit/httpx's transformer stamps the one a handler
	// returns with that request's id (kit/httpx/request_id.go), so a refusal shared
	// between two requests is one field two requests write at once, and a body that
	// quotes somebody else's request id.
	if other := ask(offlineFlags{}); other == unreadable {
		t.Error("two refusals answered with the same *problem.Problem")
	}

	// The two answers a switch can actually give are what they were before this:
	// off writes, on refuses and says where the write goes instead. A cure for the
	// outage that flipped either one would be a second way to change what the flag
	// means, so it is checked here rather than left to the end-to-end cases.
	if err := ask(configFlags(nil)); err != nil {
		t.Errorf("a switch that is off answered %v, want the settings write allowed as it was", err)
	}
	on := ask(configFlags{siteSettingsFlag: true})
	var refusal *changecontracts.Refusal
	if !errors.Is(on, crud.ErrConflict) || !errors.As(on, &refusal) || refusal.Path != proposalAddress {
		t.Errorf("a switch that is on answered %v, want the conflict whose refusal names %s", on, proposalAddress)
	}
}
