package pkit_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestACompositionProvidesItsAppSlugToTheRuntime(t *testing.T) {
	composition := pkit.NewApp("collect")
	deployment := pkit.Deployment{Environment: pkit.Development}
	if err := composition.Validate(deployment); err != nil {
		t.Fatalf("validate the named composition: %v", err)
	}
	named, ok := any(composition).(interface {
		Slug() (appname.Name, error)
	})
	if !ok {
		t.Fatal("the accepted composition cannot supply its typed app slug to the runtime")
	}
	slug, err := named.Slug()
	if err != nil || slug != appname.Name("collect") {
		t.Fatalf("composition's slug = %q, %v; want collect", slug, err)
	}
	if got := appname.Durable(slug, "ledger", "ledger.invoice_issued"); got != "collect+ledger-ledger-invoice_issued" {
		t.Errorf("composition's durable = %q", got)
	}
}

func TestACompositionRefusesAnAppNameThatCannotScopeADurable(t *testing.T) {
	if err := pkit.NewApp("Collect EU").Validate(pkit.Deployment{Environment: pkit.Development}); err == nil {
		t.Fatal("Validate accepted Collect EU, which cannot scope a runtime durable")
	}
}
