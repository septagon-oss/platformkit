package usertest

import (
	"testing"
)

func verificationCases() map[string]func(*testing.T, Fixture) {
	return map[string]func(*testing.T, Fixture){
		"mailbox verification preserves the chosen password and trusted roles": verifyMailbox,
		"verification and registration cannot replace other lifecycle states":  preserveVerificationLifecycle,
		"unverified registration validates before insertion":                   validateUnverifiedRegistration,
		// ConfirmAddress is the door the single sign-on leg walks: an authority
		// outside this application confirmed the mailbox, and this module decides
		// what that makes the person. The two refusals are the whole of the
		// security property — the address has to be this row's, and an account
		// this tenant closed or has not yet approved stays closed — so they are
		// asserted as "nothing moved and nothing was said", which is the only
		// form of the claim that a second implementation can be held to.
		"an address an identity provider confirmed activates the account it names": confirmProviderAddress,
	}
}
