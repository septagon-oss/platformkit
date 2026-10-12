package main

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/pkit"
)

func TestCompositionDescriptionRefusesAMissingMailLedgerProvider(t *testing.T) {
	cfg := referenceDependencyConfig(t)
	for _, env := range []pkit.Environment{pkit.Development, pkit.Production} {
		t.Run(string(env), func(t *testing.T) {
			description, err := sentencesOf(cfg, "notification").Describe(pkit.Deployment{Environment: env, Config: cfg})
			if err == nil {
				t.Fatal("composition described auth without its mail ledger provider")
			}
			if description == nil || description.Resolved || len(description.Modules) != 0 {
				t.Fatalf("refused composition exposed a partial module graph: %+v", description)
			}
			for _, problem := range description.Problems {
				if strings.Contains(problem.Sentence, "MailLedger") && strings.Contains(problem.Sentence, "auth") {
					return
				}
			}
			t.Fatalf("refusal does not name auth's mail ledger dependency: %+v", description.Problems)
		})
	}
}
