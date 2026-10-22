package page_test

// A plan exclusion is the application working as the plan it was bought on does. The
// guard's line about it names the feature slug the tenant's plan does not carry, which is
// what an administrator buys more of and nothing a person at the page can ask for, and
// the verdict used to be drawn and announced exactly like a handler that had broken.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
)

// TestAPlanExclusionIsWordedAsThePlanAndNotDrawnAsAFault: the page says the feature is
// not in the plan, in words, without the slug, and says it in the quiet voice — while the
// code stays on the data line for whoever reads the page back to an operator.
func TestAPlanExclusionIsWordedAsThePlanAndNotDrawnAsAFault(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://demo.localhost/admin/audit", nil)
	got := renderFault(t, shell(), &problem.Problem{
		Status: http.StatusPaymentRequired,
		Detail: httpx.CodePlanExcludes + ": this tenant's plan does not include audit-trail",
	}, req)

	if got.Code != http.StatusPaymentRequired {
		t.Errorf("the page answered %d, not the verdict the guard reached", got.Code)
	}
	body := got.Body.String()
	for _, want := range []string{
		"Not in this plan", "This isn&#39;t included in this workspace&#39;s plan.",
		`role="status"`, httpx.CodePlanExcludes,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the plan page omits %q\n%s", want, trim(body))
		}
	}
	// The slug is the plan owner's fact; the protocol's name for 402 names a payment
	// nobody at this page is being asked for; and the alert tone is what makes a
	// plan read like a breakdown.
	for _, reject := range []string{"audit-trail", "Payment Required", `role="alert"`, "1111"} {
		if strings.Contains(body, reject) {
			t.Errorf("the plan page says %q, which it must not\n%s", reject, trim(body))
		}
	}
}

// TestARefusalThisPackageHasNotReadKeepsTheSentenceThatWasWritten is the other half of
// the table: a code absent from it is shown as its writer wrote it, because a generic
// sentence standing in for an unread one hides the one thing the caller could act on.
func TestARefusalThisPackageHasNotReadKeepsTheSentenceThatWasWritten(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://demo.localhost/admin/tasks", nil)
	const line = httpx.CodeWriteElsewhere + ": this row belongs to another workspace"
	got := renderFault(t, shell(), &problem.Problem{
		Status: http.StatusConflict, Detail: line, Instance: "urn:request:cafe0000-0000-4000-8000-000000000000",
	}, req)

	if body := got.Body.String(); !strings.Contains(body, line) {
		t.Errorf("a refusal outside the table was reworded away\n%s", trim(body))
	}
}
