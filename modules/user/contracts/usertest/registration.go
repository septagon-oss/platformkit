package usertest

import (
	"testing"
)

func registrationCases() map[string]func(*testing.T, Fixture) {
	return map[string]func(*testing.T, Fixture){
		"pending registration retains a password without enabling sign-in":           registerPending,
		"approval preserves roles and password and is silent on retry":               approvePending,
		"approval cannot activate an invitation or a deactivated registration":       refuseIneligibleApproval,
		"approving an invitation names the invitation, never a password":             refuseInvitationApproval,
		"pending review pages exclude approved accounts and retain order":            pagePendingRegistrations,
		"pending registration validates credentials and refuses an existing address": validatePendingRegistration,
	}
}
