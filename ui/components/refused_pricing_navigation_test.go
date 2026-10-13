package components

import "testing"

func TestRefusedPricingClearsBillingPeriodNavigation(t *testing.T) {
	p := PricingTiersProps{Label: "Planos", State: ContentState{Status: MediaRefused, Title: "Recusado", Text: "Sem acesso."}}
	if err := p.Validate(); err != nil {
		t.Fatalf("cleared refusal must remain renderable: %v", err)
	}
	p.Periods = []ChoiceLink{{Key: "annual", Label: "Anual", Href: "/plans?account=private"}}
	if err := p.Validate(); err == nil {
		t.Fatal("refused pricing must reject retained billing-period navigation before capture")
	}
}
