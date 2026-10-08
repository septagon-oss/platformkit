package main

import (
	"context"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

// Taking user.Module out of the sentence is refused at Plan, and the refusal
// names the module to add and the modules that needed what it provided — not
// only a contract nobody can map back to a line of app.go.
func TestRemovingTheUserModuleNamesItAndWhoNeedsIt(t *testing.T) {
	cfg := referenceDependencyConfig(t)
	_, err := sentencesOf(cfg, "user").Plan(pkit.Deployment{Environment: pkit.Development, Config: cfg})
	if err == nil {
		t.Fatal("a composition without user.Module planned")
	}
	msg := err.Error()
	for _, want := range []string{
		"add user.Module to platformkit",
		"auth needs authcontracts.Users",
		"tenant needs tenantcontracts.Inviter",
		"notification needs notificationcontracts.RecipientLookup",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal does not say %q:\n%s", want, msg)
		}
	}
}

// The user module's promotion check is a holder product puts empty and auth
// fills while it builds. In the resolved reference composition it is filled:
// a caller with no principal is answered no, not the unfilled holder's error.
func TestTheReferenceCompositionFillsTheGrantCheckUserReads(t *testing.T) {
	cfg := referenceDependencyConfig(t)
	p, err := sentencesOf(cfg).Plan(pkit.Deployment{Environment: pkit.Development, Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	g, ok := pkit.Value[usercontracts.Granting](p)
	if !ok || g == nil {
		t.Fatal("the reference composition resolves no usercontracts.Granting")
	}
	may, err := g.May(context.Background(), db.Tx[db.Tenant]{})
	if err != nil {
		t.Fatalf("the grant check answers its unfilled holder's error, so auth never filled it: %v", err)
	}
	if may {
		t.Fatal("a caller with no principal may hand out an administering role")
	}
}
