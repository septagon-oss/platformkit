package internal_test

// What each command says it publishes, operation by operation.
//
// kit/httpx.Events builds the catalogue from every operation's
// x-platformkit-events list, and kit/app checks one direction at boot: an event
// a route names must be declared by some module. Nothing checks the other
// direction — that an operation which writes rows names the events its own
// handler publishes — because the aggregate answer hides it: auth-password-change
// can omit auth.session_revoked and the catalogue still lists the event, since
// auth-session-revoke names it. Review round 3 named exactly that hole: the two
// password operations described ending sessions while their event list said
// nothing about it, so a consumer that reads the served document to decide what
// to listen for never learns that a password change is a sign-out.
//
// So this case reads the recorded operations one at a time. Each row below
// quotes the sentence in the route's own Description that promises the write —
// and if that sentence is reworded or removed the row fails, so the row cannot
// quietly stop meaning anything. An operation that ends a session has to say it
// publishes the revocation.

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
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// writeCommand is one write, the promise its description makes, and the events that
// promise is published as.
type writeCommand struct {
	operation string
	promise   string
	publishes []string
}

// everyCommand is every operation in this module that changes something. A list
// route has no promise to keep and no event to publish, which is why only these
// appear here.
var everyCommand = []writeCommand{
	{"auth-login", "Opens a session", []string{contracts.EventLoggedIn}},
	{"auth-logout", "Deletes the session", []string{contracts.EventLoggedOut}},
	{"auth-password-change", "Every other session of this person ends",
		[]string{usercontracts.EventPasswordSet, contracts.EventSessionRevoked}},
	{"auth-password-reset", "Every session this person had ends",
		[]string{contracts.EventPasswordReset, usercontracts.EventPasswordSet,
			contracts.EventSessionRevoked}},
	{"auth-session-revoke", "Ends the session this person names by its ref",
		[]string{contracts.EventSessionRevoked}},
	{"auth-session-revoke-all", "Ends every session this person has",
		[]string{contracts.EventSessionRevoked}},
	{"auth-role-set", "Creates the role if it is new", []string{contracts.EventRoleSet}},
	{"auth-factor-totp-finish", "Enrols the secret",
		[]string{contracts.EventFactorEnrolled, contracts.EventRecoveryCodesIssued}},
	{"auth-factor-withdraw", "Stops one factor working", []string{contracts.EventFactorWithdrawn}},
	{"auth-recovery-codes-rotate", "issues a fresh set",
		[]string{contracts.EventRecoveryCodesIssued}},
	{"auth-token-issue", "Mints a key", []string{contracts.EventAPITokenIssued}},
	{"auth-token-revoke", "Stops one key", []string{contracts.EventAPITokenRevoked}},
}

func TestEachCommandNamesTheEventsItsOwnDescriptionPromises(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	_, _, _, api := mountRecorded(t, conn, auth.OIDC{}, false)

	recorded := map[string]*huma.Operation{}
	for _, op := range api.Recorded() {
		recorded[op.OperationID] = op
	}
	for _, c := range everyCommand {
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
