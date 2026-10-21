package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/config"
)

// commentedBudget is the example file's own commented block; each case writes
// it as a real key so the only difference from the file a reader copies is the
// budget under test.
const commentedBudget = "  # account_requests:\n  #   quota: 10\n  #   window: 15m\n"

// An omitted key, an empty block and a null value all arrive as nil: the module
// keeps its own default, and the loader writes none of its own.
func TestAnOmittedAccountRequestBudgetIsLeftToTheModule(t *testing.T) {
	for name, path := range map[string]string{
		"omitted":     example,
		"empty block": edit(t, commentedBudget, "  account_requests: {}\n"),
		"null quota":  edit(t, commentedBudget, "  account_requests:\n    quota:\n"),
	} {
		got, err := config.Load(path)
		if err != nil {
			t.Fatalf("%s: Load refused: %v", name, err)
		}
		if got.Auth.AccountRequests.Quota != nil || got.Auth.AccountRequests.Window != nil {
			t.Errorf("%s: arrived as quota %v window %v, want both nil", name, got.Auth.AccountRequests.Quota, got.Auth.AccountRequests.Window)
		}
	}
}

// A named budget is read as written, field by field, and the environment does
// not move it: a traffic decision is reviewed with the file.
func TestANamedAccountRequestBudgetIsReadAsWritten(t *testing.T) {
	t.Setenv("PLATFORMKIT_AUTH_ACCOUNT_REQUESTS_QUOTA", "3")
	got, err := config.Load(edit(t, commentedBudget, "  account_requests:\n    quota: 1000\n    window: 15m\n"))
	if err != nil {
		t.Fatalf("Load refused a named budget: %v", err)
	}
	if q, w := got.Auth.AccountRequests.Quota, got.Auth.AccountRequests.Window; q == nil || *q != 1000 || w == nil || *w != 15*time.Minute {
		t.Errorf("quota 1000 and window 15m arrived as %v and %v", q, w)
	}
	got, err = config.Load(edit(t, commentedBudget, "  account_requests:\n    quota: 1000\n"))
	if err != nil {
		t.Fatalf("Load refused a quota alone: %v", err)
	}
	if q, w := got.Auth.AccountRequests.Quota, got.Auth.AccountRequests.Window; q == nil || *q != 1000 || w != nil {
		t.Errorf("a quota alone arrived as %v and window %v, want 1000 and nil", q, w)
	}
}

// A present value that counts nothing is refused by name, never read as
// unlimited; a window with no unit is the decoder's refusal.
func TestAnAccountRequestBudgetThatCountsNothingIsRefused(t *testing.T) {
	for _, tt := range []struct{ block, names string }{
		{"  account_requests:\n    quota: 0\n", "auth.account_requests.quota"},
		{"  account_requests:\n    quota: -3\n", "auth.account_requests.quota"},
		{"  account_requests:\n    window: 0s\n", "auth.account_requests.window"},
		{"  account_requests:\n    window: -1m\n", "auth.account_requests.window"},
		{"  account_requests:\n    window: 900\n", "time.Duration"},
	} {
		_, err := config.Load(edit(t, commentedBudget, tt.block))
		if err == nil {
			t.Errorf("Load accepted %q", tt.block)
		} else if !strings.Contains(err.Error(), tt.names) {
			t.Errorf("Load(%q) = %v, want the refusal to name %s", tt.block, err, tt.names)
		}
	}
}
