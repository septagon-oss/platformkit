package rest_test

// A pause between sets states its length twice: the number the script counts
// down, and, above it, the same length in words. Past one hour the words change
// shape — "1:02:05", not "62:05" — into the shape the script's own clock prints,
// so the line read before any script runs is the line the countdown keeps.

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/rest"
)

// TestTheRestPagePastAnHourSaysItsLengthInHours checks the written length of a
// pause the session declares longer than an hour. Nothing in the page narrows
// that declared number: the seconds it counts are the ones the step declared, and
// the words over them switch to hours at the same instant the browser's countdown
// does. The pair under the pause is the fixture's own between-sets one, recorded
// through the native form, so only the declared length differs from the pause the
// shorter cases read.
func TestTheRestPagePastAnHourSaysItsLengthInHours(t *testing.T) {
	const pause = 3725
	h := newGuided(t, func(spec *rest.SessionSpec) {
		loaded := spec.Session.Load
		spec.Session.Load = func(ctx context.Context, tx db.Tx[db.Tenant], actor, id uuid.UUID) (entity.GuidedSession, error) {
			session, err := loaded(ctx, tx, actor, id)
			if err != nil {
				return session, err
			}
			session.Steps[1].RestBetweenSeconds = pause
			return session, nil
		}
	})
	mountGuidedScreens(h)
	web := "/app/practice/sessions/" + guidedID.String()
	recordGuidedSets(t, h, web, 0, 1)

	step := guidedFixture().Steps[1]
	res := guidedCall(h.router, "GET", fmt.Sprintf("%s/rest/%s/%s", web, step.ID, step.Sets[0]), "", "", "")
	if res.Code != 200 {
		t.Fatalf("the pause after a recorded set: %d %s", res.Code, res.Body.String())
	}
	body := res.Body.String()
	if !strings.Contains(body, fmt.Sprintf(`data-guided-seconds="%d"`, pause)) {
		t.Errorf("the pause counts %v, want the %d the step declared",
			regexp.MustCompile(`data-guided-seconds="([^"]*)"`).FindStringSubmatch(body), pause)
	}
	if !strings.Contains(body, ">1:02:05<") {
		t.Errorf("the pause reads %v, want the hour form 1:02:05 of the same %d seconds",
			regexp.MustCompile(`>(\d{1,3}:\d{2}(?::\d{2})?)<`).FindAllString(body, -1), pause)
	}
}
