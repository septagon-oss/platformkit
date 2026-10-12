package main

import (
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/pkit"
)

func TestCompositionDescriptionNamesTheMailLedgerProvider(t *testing.T) {
	cfg := referenceDependencyConfig(t)
	for _, env := range []pkit.Environment{pkit.Development, pkit.Production} {
		t.Run(string(env), func(t *testing.T) {
			description, err := sentencesOf(cfg).Describe(pkit.Deployment{Environment: env, Config: cfg})
			if err != nil {
				t.Fatal(err)
			}
			var provided, required bool
			for _, module := range description.Modules {
				switch module.Name {
				case "notification":
					provided = slices.Contains(module.Provides, "notificationcontracts.MailLedger")
				case "auth":
					required = slices.Contains(module.Needs, pkit.DescribedNeed{
						Contract: "notificationcontracts.MailLedger", From: "notification",
					})
				}
			}
			if !description.Resolved || !provided || !required {
				t.Fatalf("mail ledger edge: resolved=%v, notification provides=%v, auth requires=%v", description.Resolved, provided, required)
			}
		})
	}
}
