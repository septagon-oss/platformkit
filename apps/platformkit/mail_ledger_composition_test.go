package main

import (
	"strings"
	"testing"

	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestAuthResolvesTheNotificationServicesMailLedger(t *testing.T) {
	cfg := referenceDependencyConfig(t)
	deployment := pkit.Deployment{Environment: pkit.Development, Config: cfg}
	plan, err := sentencesOf(cfg).Plan(deployment)
	if err != nil {
		t.Fatal(err)
	}
	ledger, ok := pkit.Value[authcontracts.MailLedger](plan)
	if !ok || ledger == nil {
		t.Fatal("auth has no mail ledger in the resolved composition")
	}
	notices, ok := pkit.Value[notificationcontracts.Service](plan)
	if !ok || notices == nil || ledger != authcontracts.MailLedger(notices) {
		t.Fatal("auth's ledger is not the notification service that owns delivery records")
	}
	_, err = sentencesOf(cfg, "notification").Plan(deployment)
	if err == nil {
		t.Fatal("auth planned without the module that records its mail")
	}
	if !strings.Contains(err.Error(), "MailLedger") || !strings.Contains(err.Error(), "auth") {
		t.Fatalf("missing notification provider did not name auth's mail ledger dependency: %v", err)
	}
}
