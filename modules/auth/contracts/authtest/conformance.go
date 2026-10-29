// Package authtest is the conformance suite for contracts.Service, a fake that
// passes it, and an OpenID Connect issuer a test can sign in against.
package authtest

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// Fixture is one case's world. The tenant already has the two roles SeedRoles
// installs, because that is what the tenant module's create hook does and every
// tenant that exists has been through it.
type Fixture struct {
	Ctx     context.Context
	Tx      db.Tx[db.Tenant]
	Service contracts.Service
	// User creates a user and returns their id. An empty password makes
	// somebody who has been invited and cannot sign in yet.
	User func(email, password string, roles ...string) uuid.UUID
	// Role grants permissions to a role name in this tenant.
	Role func(name string, permissions ...string)
	// Published is the names of the events published so far, in order.
	Published func() []string
	// Sent is every notice the implementation asked to be delivered, in order.
	// A case reads one to check what it does not carry.
	Sent func() []notificationcontracts.Notice
	// Mailed is every message the implementation handed a mail server, in
	// order. A case reads the link out of one to follow it, and it is the only
	// place it can be read — which is the property, not an inconvenience.
	Mailed func() []notificationcontracts.Message
	// Settle runs step once the case's transaction has committed, in a
	// transaction of its own over the same rows. A mailed link leaves with the
	// commit and not with the transaction (kit/db AfterCommit), so the step is
	// where a case reads one and spends it. The case's own work ends at the
	// call: what it wrote is what the step reads, and a step's own writes are
	// rolled back, so a step settles no further. Every harness says what
	// settling means for it; a case given none is failed rather than skipped.
	Settle func(step func(Fixture))
	// Sessions reports how many sessions this user has, which is how a case
	// says "and the others ended" without knowing how they are stored.
	Sessions func(user uuid.UUID) int
	// Delivering moves this case's installation between a sender that attempts the
	// message and one that only records it — false is the deployment with no mail
	// host, where a door that would have to promise a person an email refuses.
	// A harness wires it to whatever its own carrier answers; a case that never
	// calls it leaves the installation as it found it.
	Delivering func(delivers bool)
}

// Declared is the permission catalogue the cases hand to SetRole: two ordinary
// permissions and one operator's. It stands in for what the kernel reads off
// every manifest, and the names are deliberately not any real module's — what
// is under test is the rule, not the catalogue.
var Declared = []tenancy.Grant{
	{Permission: "widget:read"},
	{Permission: "widget:manage"},
	{Permission: "fleet:manage", Operator: true},
}

// Harness builds one Fixture and calls run with it.
type Harness func(t *testing.T, run func(Fixture))

// RunService is the conformance suite. Every implementation of
// contracts.Service passes it, or it is not one.
func RunService(t *testing.T, h Harness) {
	t.Helper()
	for name, run := range cases() {
		t.Run(name, func(t *testing.T) {
			h(t, func(f Fixture) { run(t, f) })
		})
	}
}

// The two passwords every case uses.
const (
	Password = "correct horse battery staple"
	Wrong    = "incorrect horse battery staple"
)

// nobody is what a session opened for nobody is called.
var nobody = contracts.Client{UserAgent: "conformance", IP: "203.0.113.1"}

func cases() map[string]func(*testing.T, Fixture) {
	return map[string]func(*testing.T, Fixture){
		"a password opens a session":                                                           loginWithPassword,
		"an address that differs only in case is the same person":                              loginWithFoldedAddress,
		"a wrong password and an address nobody has answer alike":                              refuseInvalidCredentials,
		"somebody who has no password cannot sign in":                                          refusePasswordlessLogin,
		"too many failures locks the address, correct password included":                       lockFailedLogins,
		"a session recognises its owner, and an unknown one does not":                          identifySession,
		"logging out ends the session, and again says nothing":                                 logoutSession,
		"a role grants exactly what it names":                                                  grantRolePermissions,
		"a session opened for somebody already recognised is a session":                        openRecognisedSession,
		"changing a password needs the one in force, and ends the other sessions":              changePassword,
		"asking to reset a password does the same work whoever asks":                           requestPasswordReset,
		"where no mail is delivered a reset is refused and nothing is written":                 refuseResetWithoutMail,
		"the worker looks the address up, and mails a link only to somebody who is here":       mailPasswordReset,
		"one link per person per interval, however often somebody asks":                        rateLimitPasswordLinks,
		"the link sets a password once, ends every session it found and signs this browser in": resetPassword,
		"an invitation is offered a link, and somebody who can already sign in is not":         offerInvitationLink,
		"a role grants only permissions the application defines":                               refuseUndeclaredPermission,
		"role normalization is idempotent and rejects invalid names":                           normaliseRole,
		"the role write says whether the row changed":                                          reportRoleChange,
		"the last role that can administer roles cannot be emptied":                            preserveLastRole,
		"a role name is bounded":                                                               boundRoleName,
		"an operator permission cannot be granted in a customer's tenant":                      refuseOperatorPermission,
		"a sessions list names each session by its ref and never by its id":                    listSessionRefs,
		"a ref revokes that session and leaves the others":                                     revokeSessionRef,
		"revoking everything ends the caller's own session too":                                revokeAllSessions,
		"a ref that is nobody's revokes nothing and publishes nothing":                         refuseUnknownSessionRef,
	}
}

// notices is what the implementation asked to have delivered, or nothing when
// the harness wired no recorder.
func notices(f Fixture) []notificationcontracts.Notice {
	if f.Sent == nil {
		return nil
	}
	return f.Sent()
}

// mailed is what the implementation handed a mail server, or nothing when the
// harness wired no mailbox.
func mailed(f Fixture) []notificationcontracts.Message {
	if f.Mailed == nil {
		return nil
	}
	return f.Mailed()
}

// settle hands the rest of a case to the point after its commit, which is the
// first point at which a link the person was mailed exists to be read. A case
// that reads one calls this rather than reading the mailbox beside the mint
// that has not left yet.
func settle(t *testing.T, f Fixture, step func(Fixture)) {
	t.Helper()
	if f.Settle == nil {
		t.Fatalf("%s reads what the implementation mails, and this harness wires no Settle to read it after the commit", t.Name())
	}
	f.Settle(step)
}

// sessions is how many the user has, or -1 when the harness cannot say.
func sessions(f Fixture, user uuid.UUID) int {
	if f.Sessions == nil {
		return -1
	}
	return f.Sessions(user)
}

func published(t *testing.T, f Fixture, want ...string) {
	t.Helper()
	if f.Published == nil {
		return
	}
	if got := f.Published(); !slices.Equal(got, want) {
		t.Errorf("published %v, want %v", got, want)
	}
}
