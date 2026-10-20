package admin_test

import (
	"net/http"
	"strings"
	"testing"
)

// TestTheWorkspaceScopeFollowsTheHostNotThePerson: the same caller, holding
// every grant, is a tenant member on a customer's host and an operator only on
// the installation's. The customer's home names no operator scope and links to
// no operator-only destination; the operator's home does both.
func TestTheWorkspaceScopeFollowsTheHostNotThePerson(t *testing.T) {
	router := mount(t)
	code, customer, _ := callAt(t, router, host, http.MethodGet, "/app", "")
	if code != http.StatusOK || !strings.Contains(customer, "Acme") {
		t.Fatalf("the customer's home = %d, and does not name its tenant: %s", code, customer)
	}
	if !strings.Contains(customer, "Tenant workspace") {
		t.Error("the customer's home does not say it is a tenant workspace")
	}
	if strings.Contains(customer, "Platform operator") || strings.Contains(customer, "Add a plan") {
		t.Error("the customer's home offers operator scope on a customer's host")
	}
	code, installation, _ := callAt(t, router, operatorHost, http.MethodGet, "/app", "")
	if code != http.StatusOK || !strings.Contains(installation, "Platform operator") {
		t.Fatalf("the operator's home = %d, and does not name the operator scope: %s", code, installation)
	}
	if !strings.Contains(installation, "Add a plan") {
		t.Error("the operator's home hides the operator-only destination the composition declares")
	}
}
