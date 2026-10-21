package auth_test

import (
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// A budget the counter cannot keep is refused by New, naming the field; the
// zero value and a large quota build.
func TestNewRefusesAnAccountRequestBudgetTheCounterCannotKeep(t *testing.T) {
	for _, budget := range []contracts.AccountRequestBudget{
		{Quota: -1},
		{Window: -time.Minute},
		{Window: 48 * time.Hour},
	} {
		if said := refusal(auth.Deps{AccountRequests: budget}); !strings.Contains(said, "AccountRequests") {
			t.Errorf("New(AccountRequests: %+v) answered %q, want a refusal naming AccountRequests", budget, said)
		}
	}
	for _, budget := range []contracts.AccountRequestBudget{{}, {Quota: 1000}} {
		if said := refusal(auth.Deps{AccountRequests: budget}); said != "" {
			t.Errorf("New(AccountRequests: %+v) refused: %s", budget, said)
		}
	}
}
