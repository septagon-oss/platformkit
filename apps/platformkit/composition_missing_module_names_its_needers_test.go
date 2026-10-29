package main

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

// Taking user.Module out of the sentence is refused at Plan, and the refusal
// names the module to add and the modules that needed what it provided — not
// only a contract nobody can map back to a line of app.go.
func TestRemovingTheUserModuleNamesItAndWhoNeedsIt(t *testing.T) {
	cfg := referenceCompositionConfig(t)
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

// The independent promotion policy is ready at construction and refuses an
// anonymous caller in the explicit tenant transaction.
func TestTheReferenceCompositionFillsTheGrantCheckUserReads(t *testing.T) {
	cfg := referenceCompositionConfig(t)
	p, err := sentencesOf(cfg).Plan(pkit.Deployment{Environment: pkit.Development, Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	g, ok := pkit.Value[usercontracts.Granting](p)
	if !ok || g == nil {
		t.Fatal("the reference composition resolves no usercontracts.Granting")
	}
	_, conn := dbtest.Schema(t)
	err = db.Run(tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: uuid.New()}), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		may, err := g.May(ctx, tx)
		if may {
			t.Fatal("a caller with no principal may hand out an administering role")
		}
		return err
	})
	if err != nil {
		t.Fatalf("the initialized grant check failed: %v", err)
	}
}
