package admin_test

// A pin over how loud the sessions screen is allowed to be.
//
// The screen answers "was that me?" for every machine a person is signed in on,
// so it can offer several revocations at once. Drawing each of them as a filled
// danger button made the page three identical calls to action above the fold —
// the design floor refuses more than one primary action per view for the reason
// a person sees first: with three red buttons the page has not decided which
// click it is for, and the one that ends the session being read is a button like
// the others. The rule is therefore about prominence and not about labels: the
// words on a revocation still have to name the row it belongs to.
//
// Three counts on the drawn page, all about the markup a browser would paint:
//
//   - exactly one filled submit control, which is the page's own action of
//     ending every session but this one;
//   - one quiet submit control per row that is not the caller's, each naming the
//     device it ends, so a quiet action is not an anonymous one;
//   - every quiet action still naming its row out loud, since the accessible
//     name is what a person who does not see the table is deciding on.

import (
	"net/http"
	"strings"
	"testing"
)

func TestTheSessionsScreenAsksForOneClickAtATime(t *testing.T) {
	store := threeSessions()
	router := mountAs(t, caller{}, withSessions(store))

	res := signedInAs(t, router, http.MethodGet, "/app/auth/sessions", "")
	if res.Code != http.StatusOK {
		t.Fatalf("the sessions screen = %d %s", res.Code, res.Body.String())
	}
	body := res.Body.String()

	// The page's single loud action: a filled submit button. Every row's
	// revocation is drawn quiet, so the one that stands out is the one the
	// toolbar describes.
	if got := submitCount(body, `data-variant="primary"`); got != 1 {
		t.Errorf("%d filled submit controls are drawn, want the 1 the screen is for: ending every session but this one", got)
	}
	// threeSessions has three rows, and every one of them can be ended from its
	// own line — including the session that is asking, which the page ends by its
	// own button rather than by a bulk action that would sign the person out of
	// the page they are reading.
	if got := submitCount(body, `data-variant="secondary"`); got != 3 {
		t.Errorf("%d quiet row actions, want one per session listed", got)
	}
	for _, want := range []string{"End the session on Firefox on a laptop", "End the session on Safari on a phone"} {
		if !strings.Contains(body, `aria-label="`+want+`"`) {
			t.Errorf("no revocation names %q, so a quiet action became an anonymous one", want)
		}
	}
}

// submitCount is how many submit buttons carry one variant attribute. The
// attribute belongs to the element, so counting the pairs that appear on the
// same tag is the count of painted buttons and not of the words near them.
func submitCount(body, variant string) int {
	counted := 0
	for _, part := range strings.Split(body, "<button") {
		if !strings.Contains(part, variant) {
			continue
		}
		tag := part
		if end := strings.Index(tag, ">"); end >= 0 {
			tag = tag[:end]
		}
		if strings.Contains(tag, `type="submit"`) {
			counted++
		}
	}
	return counted
}
