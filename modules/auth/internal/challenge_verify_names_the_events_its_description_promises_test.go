package internal_test

// The two write operations this module's own promise-and-event table leaves out.
//
// declared_events_test.go states the rule it enforces: an operation that writes
// rows must name, in its x-platformkit-events list, the events its own
// Description promises, and the promise is quoted from that Description so a
// reworded sentence fails the row rather than passing it quietly. Its
// `everyCommand` is introduced as "every operation in this module that changes
// something", and it lists twelve. The module answers more than twelve writes:
//
//   $ awk '/^var everyCommand/,/^}$/' modules/auth/internal/declared_events_test.go \
//         | grep -oE '\{"auth-[a-z-]+"' | wc -l
//     12
//
// The two missing here are the ones mounted by the plain composition that table
// already reads — one of them is the second factor's own door, which this branch
// added and which is the only operation in this module that opens a session
// outside a password:
//
//   auth-challenge-verify  auth.logged_in, auth.login_failed, auth.recovery_code_used
//   auth-password-forgot   auth.reset_requested
//
// Nothing about their behaviour is wrong today: both declare their events and the
// checked-in contract carries them. What is missing is the guard. The aggregate
// checks cannot see a dropped one — TestEveryDeclaredEventIsInTheDocumentAndCovered
// is satisfied while the event is declared *somewhere* — so an edit that took
// `auth.logged_in` off the challenge door would leave every case in this
// repository green while a consumer reading the served document stopped learning
// that this address signs people in. That is the exact hole round 3 wrote the
// table to close, opened again by the operation left out of it.
//
// A row fails two ways, and both are wanted: the sentence must still be in the
// Description (a promise removed is a promise unkept), and the event must still be
// in the list the served document shows.

import (
	"slices"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

// unwrittenCommands are the writes of this composition that
// declared_events_test.go's table does not carry, with the sentence in each
// route's own Description that promises the write.
var unwrittenCommands = []struct {
	operation string
	promise   string
	publishes []string
}{
	{
		"auth-challenge-verify",
		"opens the session the first half had already earned",
		[]string{contracts.EventLoggedIn, contracts.EventLoginFailed, contracts.EventRecoveryCodeUsed},
	},
	{
		"auth-password-forgot",
		"this route publishes one event",
		[]string{contracts.EventResetRequested},
	},
}

func TestTheSecondHalfNamesTheEventsItsOwnDescriptionPromises(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	_, _, _, api := mountRecorded(t, conn, auth.OIDC{}, false)

	recorded := map[string]*huma.Operation{}
	for _, op := range api.Recorded() {
		recorded[op.OperationID] = op
	}
	for _, c := range unwrittenCommands {
		op, ok := recorded[c.operation]
		if !ok {
			t.Errorf("%s is not mounted, so this table names an operation the module does not answer with",
				c.operation)
			continue
		}
		if !strings.Contains(op.Description, c.promise) {
			t.Errorf("%s: the description no longer says %q, so this row is checking nothing; the events it "+
				"lists are still owed whatever the sentence says now (%q)",
				c.operation, c.promise, op.Description)
		}
		named, _ := op.Extensions[httpx.EventsExtension].([]string)
		for _, event := range c.publishes {
			if !slices.Contains(named, event) {
				t.Errorf("%s: %q, but the operation declares %v and not %q — a consumer that reads the "+
					"served document never learns to listen for it",
					c.operation, c.promise, named, event)
			}
		}
	}
}
