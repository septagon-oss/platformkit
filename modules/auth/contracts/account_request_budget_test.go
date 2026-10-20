package contracts_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/limit"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// The cases run over kit/limit's memory store, the one the fake runs: the decision
// is contracts.Limiter's, the same code the Postgres service runs, and what the
// store does is proved in kit/limit's own tests.

// A budget of N admits N account requests from one address in a window and refuses
// the next, naming the configured quota and window; another address is untouched.
// Twelve over the default ten is the case that fails when the budget is ignored.
func TestABudgetAdmitsItsQuotaAndRefusesTheNext(t *testing.T) {
	for _, quota := range []int{3, contracts.ResetRequests + 2} {
		l := contracts.NewBudgetedLimiter(limit.Memory(), contracts.AccountRequestBudget{Quota: quota, Window: time.Minute})
		ctx := context.Background()
		want := contracts.Policy{Name: contracts.AccountRequestsPolicyName, Quota: quota, Window: time.Minute}
		for i := range quota {
			d := l.Requested(ctx, "203.0.113.7")
			if !d.Allowed || d.Policy != want || d.Remaining != quota-i-1 {
				t.Fatalf("quota %d, request %d = %+v, want allowed under %+v with %d left", quota, i+1, d, want, quota-i-1)
			}
		}
		d := l.Requested(ctx, "203.0.113.7")
		if d.Allowed || d.Policy != want || d.Remaining != 0 || d.Left <= 0 || d.Left > time.Minute {
			t.Errorf("quota %d, request %d = %+v, want refused under %+v, nothing left, a wait inside the window", quota, quota+1, d, want)
		}
		if other := l.Requested(ctx, "203.0.113.8"); !other.Allowed {
			t.Errorf("quota %d: another address was refused: %+v; the budget is per address", quota, other)
		}
	}
}

// Each omitted field keeps its own default, field by field: the figures are
// written as literals so a moved constant fails here.
func TestAnOmittedBudgetFieldKeepsItsDefault(t *testing.T) {
	for name, tt := range map[string]struct {
		limiter *contracts.Limiter
		want    contracts.Policy
	}{
		"NewLimiter": {contracts.NewLimiter(limit.Memory()),
			contracts.Policy{Name: contracts.AccountRequestsPolicyName, Quota: 10, Window: 15 * time.Minute}},
		"zero budget": {contracts.NewBudgetedLimiter(limit.Memory(), contracts.AccountRequestBudget{}),
			contracts.Policy{Name: contracts.AccountRequestsPolicyName, Quota: 10, Window: 15 * time.Minute}},
		"quota alone": {contracts.NewBudgetedLimiter(limit.Memory(), contracts.AccountRequestBudget{Quota: 1000}),
			contracts.Policy{Name: contracts.AccountRequestsPolicyName, Quota: 1000, Window: 15 * time.Minute}},
		"window alone": {contracts.NewBudgetedLimiter(limit.Memory(), contracts.AccountRequestBudget{Window: time.Hour}),
			contracts.Policy{Name: contracts.AccountRequestsPolicyName, Quota: 10, Window: time.Hour}},
	} {
		if got := tt.limiter.Requested(context.Background(), "203.0.113.7"); got.Policy != tt.want || !got.Allowed {
			t.Errorf("%s: Requested = %+v, want allowed under %+v", name, got, tt.want)
		}
	}
}

// Check refuses what the counter cannot keep and names the field; the zero value
// and every budget inside the counter's horizon and ceiling pass.
func TestABudgetTheCounterCannotKeepIsRefused(t *testing.T) {
	for _, tt := range []struct {
		budget contracts.AccountRequestBudget
		names  string
	}{
		{contracts.AccountRequestBudget{Quota: -1}, "quota"},
		{contracts.AccountRequestBudget{Quota: limit.MaxQuota + 1}, "quota"},
		{contracts.AccountRequestBudget{Window: -time.Second}, "window"},
		{contracts.AccountRequestBudget{Window: limit.MaxWindow + time.Second}, "window"},
	} {
		err := tt.budget.Check()
		if err == nil || !strings.Contains(err.Error(), tt.names) {
			t.Errorf("Check(%+v) = %v, want a refusal naming the %s", tt.budget, err, tt.names)
		}
	}
	for _, budget := range []contracts.AccountRequestBudget{
		{},
		{Quota: 1, Window: time.Millisecond},
		{Window: limit.MaxWindow},
		{Quota: 1000},
		{Quota: limit.MaxQuota},
	} {
		if err := budget.Check(); err != nil {
			t.Errorf("Check(%+v) refused: %v", budget, err)
		}
	}
}

// The budget moves the account-request counter and no other: the redemption cap,
// the lockout and the passkey prompts keep their own numbers and window.
func TestTheBudgetMovesNoOtherCounter(t *testing.T) {
	l := contracts.NewBudgetedLimiter(limit.Memory(), contracts.AccountRequestBudget{Quota: 1, Window: time.Second})
	ctx := context.Background()
	if got, want := l.Redeemed(ctx, "203.0.113.7").Policy, (contracts.Policy{
		Name: contracts.ResetRedemptionsPolicyName, Quota: contracts.ResetRedemptions, Window: contracts.AttemptWindow,
	}); got != want {
		t.Errorf("Redeemed states %+v, want %+v", got, want)
	}
	for range contracts.MaxAttempts {
		l.Failed(ctx, "ada@acme.example.com", "203.0.113.7")
	}
	if _, got := l.Check(ctx, "ada@acme.example.com", "203.0.113.7"); got.Policy != (contracts.Policy{
		Name: contracts.LoginAttemptsPolicyName, Quota: contracts.MaxAttempts, Window: contracts.AttemptWindow,
	}) {
		t.Errorf("the lockout states %+v, want its own %d in %s", got.Policy, contracts.MaxAttempts, contracts.AttemptWindow)
	}
	for i := range contracts.PasskeyPrompts {
		if !l.Prompted(ctx, "203.0.113.7") {
			t.Fatalf("passkey prompt %d refused under a budget that is not its own", i+1)
		}
	}
	if l.Prompted(ctx, "203.0.113.7") {
		t.Errorf("passkey prompt %d allowed; PasskeyPrompts is unchanged", contracts.PasskeyPrompts+1)
	}
}
