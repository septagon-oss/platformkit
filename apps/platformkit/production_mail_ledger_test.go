package main

import (
	"testing"

	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestProductionAuthUsesTheDeliveryLedger(t *testing.T) {
	cfg := referenceDependencyConfig(t)
	deployment := pkit.Deployment{Environment: pkit.Production, Config: cfg}
	plan, err := sentencesOf(cfg).Plan(deployment)
	if err != nil {
		t.Fatal(err)
	}
	ledger, ok := pkit.Value[authcontracts.MailLedger](plan)
	if !ok || ledger == nil {
		t.Fatal("production auth has no delivery ledger")
	}
	notices, ok := pkit.Value[notificationcontracts.Service](plan)
	if !ok || notices == nil || ledger != authcontracts.MailLedger(notices) {
		t.Fatal("production auth does not use notification's delivery ledger")
	}
	if _, err := sentencesOf(cfg, "notification").Plan(deployment); err == nil {
		t.Fatal("production auth planned without its delivery ledger provider")
	}
}
