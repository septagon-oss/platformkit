package billingtest_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/billing/contracts"
	"github.com/septagon-oss/platformkit/modules/billing/contracts/billingtest"
)

// TestFakeConforms runs the suite against the fake. This is what makes the fake
// worth having: a consumer that tests against it is testing against the same
// rules internal/service_test.go proves the real service keeps.
func TestFakeConforms(t *testing.T) {
	billingtest.RunService(t, func(t *testing.T, run func(billingtest.Fixture)) {
		fake := billingtest.NewFake()
		run(billingtest.Fixture{
			Ctx: t.Context(), Service: fake,
			Plan: fake.Put, Expire: fake.Expire, Reprice: fake.Reprice,
			Age: fake.Age, Published: fake.Published,
		})
	})
}

// TestFakeRecordsWhatItWouldPublish: the one thing the fake offers over the real
// service, for a consumer asserting on what a subscription did rather than on
// what it is.
func TestFakeRecordsWhatItWouldPublish(t *testing.T) {
	fake := billingtest.NewFake()
	plan := fake.Put(&contracts.Plan{Code: "pro", Name: "Pro", PriceCents: 2900, Currency: "EUR", Active: true})

	for range 2 { // the second one is the same intention, so it says nothing
		if _, err := fake.Subscribe(t.Context(), db.Tx[db.Tenant]{}, plan); err != nil {
			t.Fatalf("Subscribe: %v", err)
		}
	}
	if _, err := fake.Cancel(t.Context(), db.Tx[db.Tenant]{}, false); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	want := []string{contracts.EventSubscribed, contracts.EventCancelled}
	got := fake.Published()
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("the fake published %v, want %v", got, want)
	}
	sub, err := fake.Current(t.Context(), db.Tx[db.Tenant]{})
	if err != nil || sub.Status != contracts.StatusCancelled {
		t.Errorf("the store holds %v, %v; want the cancelled subscription", sub, err)
	}
}
