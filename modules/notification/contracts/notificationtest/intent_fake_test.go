package notificationtest

import "testing"

// The fake passes the suite the SQL service passes. What it cannot be made to show
// — which transaction wrote the row, and what a process that died in the middle of a
// send left behind — is in the module's own suite over a real database.
func TestTheFakeMailIntentsOweWhatTheyAreTold(t *testing.T) {
	RunMailIntents(t, func(t *testing.T, run func(IntentFixture)) {
		run(IntentFixture{Ctx: t.Context(), Intents: NewFakeMailIntents()})
	})
}
