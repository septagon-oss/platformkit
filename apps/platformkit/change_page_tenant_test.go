package main

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// Another tenant's administrator holds every change grant in their own tenant, and the
// review pages answer them about acme's proposal exactly as they answer about a proposal
// that does not exist: not found, and nothing decided.
func TestAnotherTenantsReviewPageReachesNoProposal(t *testing.T) {
	cfg, _, _, _ := taskChangeFixture(t, true)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	task := newTask(t, cfg, admin, "A task of acme's", "normal")
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, proposalsPath,
		`{"subjectModule":"task","subjectEntity":"task","subjectId":"`+task+`","diff":{"priority":"high"},"summary":"Inspect sooner"}`)
	if code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("propose = %d %s", code, body)
	}
	pid := field(t, body, "id")

	code, body = do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath,
		`{"slug":"globex","name":"Globex","host":"`+globexHost+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", tenantPath, code, body)
	}
	provision(t, cfg, uuid.MustParse(field(t, body, "id")), "root@globex.localhost")
	globex := signIn(t, cfg, globexHost, "root@globex.localhost", adminPass)

	if status, _, _ := askNavigate(t, cfg, globex, globexHost, "/app/change/proposals/"+pid); status != http.StatusNotFound {
		t.Errorf("globex reads acme's proposal page = %d, want 404", status)
	}
	for _, post := range []struct{ path, form string }{
		{"/review", "verdict=approved&expectedRevision=1"},
		{"/withdraw", "expectedRevision=1"},
		{"/apply", "expectedRevision=1"},
	} {
		status, _, _ := askNavigateRaw(t, cfg, globex, http.MethodPost, globexHost,
			"/app/change/proposals/"+pid+post.path, "application/x-www-form-urlencoded", post.form)
		if status != http.StatusNotFound {
			t.Errorf("globex POST %s on acme's proposal = %d, want 404", post.path, status)
		}
	}
	code, body = do(t, cfg, admin, http.MethodGet, acmeHost, proposalsPath+"/"+pid, "")
	if code != http.StatusOK || field(t, body, "state") != "proposed" || fieldNumber(t, body, "revision") != 1 {
		t.Errorf("acme's proposal moved after another tenant's posts: %d %s", code, body)
	}
}
