package main

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/pkit"
)

// A missing provider or two signup policies must fail before even the migration
// ledger exists. Exercise Build, not only Plan, over the reference sentence.
func TestCompositionRefusalLeavesTheDatabaseEmpty(t *testing.T) {
	for _, name := range []string{"missing user", "two registration doors"} {
		t.Run(name, func(t *testing.T) {
			cfg := referenceDependencyConfig(t)
			cfg.Database.MigrateURL, cfg.Database.URL = dbtest.URLs(t)
			deployment := pkit.Deployment{Environment: pkit.Development, Config: cfg}
			if _, err := sentencesOf(cfg).Plan(deployment); err != nil {
				t.Fatalf("the complete reference sentence does not plan: %v", err)
			}
			a := sentencesOf(cfg, "user")
			want := []string{"auth needs authcontracts.Users", "add user.Module to platformkit"}
			if name == "two registration doors" {
				a = sentencesOf(cfg).Use(auth.Registration)
				want = []string{"RegistrationMode", "registration", "emailregistration", "Choose"}
			}
			runtime, err := a.Build(t.Context(), deployment)
			if runtime != nil {
				defer runtime.Close()
				t.Error("a refused composition returned a runtime")
			}
			if err == nil {
				t.Error("the invalid composition built successfully")
			} else {
				for _, text := range want {
					if !strings.Contains(err.Error(), text) {
						t.Errorf("refusal does not name %q: %v", text, err)
					}
				}
			}
			owner := dbtest.Open(t, cfg.Database.MigrateURL)
			var tables int
			if err := owner.QueryRowContext(t.Context(), `SELECT count(*)
				FROM pg_tables WHERE schemaname = current_schema()`).Scan(&tables); err != nil {
				t.Fatal(err)
			}
			if tables != 0 {
				t.Errorf("refused Build created %d tables; no ledger, domain row or outbox may be written", tables)
			}
		})
	}
}
