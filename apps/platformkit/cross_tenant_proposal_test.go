package main

// A proposal is one tenant's row. Row-level security narrows change_proposals to
// the tenant the request resolved to, and modules/change/internal proves that on the
// service; this case proves it on the composed doors, where the tenant comes from
// the host and the actor from a session. Globex's own administrator — every grant
// Globex has, change:decide included — asks for Acme's proposal by its id through
// each of the five doors that take one, and every door answers as if there were no
// such row. Then Acme reads its proposal back and finds it exactly as it left it,
// and Acme's own decider still decides it: the refusals above are about the tenant,
// not about a broken row.

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestAnotherTenantCannotReachAProposal(t *testing.T) {
	cfg, _, _, _ := changeFixture(t, false)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, proposalsPath,
		`{"subjectModule":"site","subjectEntity":"settings",`+
			`"subjectId":"00000000-0000-0000-0000-000000000000",`+
			`"diff":{"title":"Acme, only Acme decides"},"summary":"one tenant's change"}`)
	if code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want the proposal recorded", proposalsPath, code, body)
	}
	id := field(t, body, "id")

	code, body = do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath,
		`{"slug":"globex","name":"Globex","host":"`+globexHost+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", tenantPath, code, body)
	}
	provision(t, cfg, uuid.MustParse(field(t, body, "id")), "root@globex.localhost")
	globex := signIn(t, cfg, globexHost, "root@globex.localhost", adminPass)

	for _, door := range []struct{ method, path, body string }{
		{http.MethodGet, proposalsPath + "/" + id, ""},
		{http.MethodPost, proposalsPath + "/" + id + "/review", `{"verdict":"approved","expectedRevision":1}`},
		{http.MethodPost, proposalsPath + "/" + id + "/apply", `{"expectedRevision":1}`},
		{http.MethodPost, proposalsPath + "/" + id + "/apply", `{"expectedRevision":2}`},
		{http.MethodPost, proposalsPath + "/" + id + "/withdraw", `{"expectedRevision":1}`},
	} {
		if code, body := do(t, cfg, globex, door.method, globexHost, door.path, door.body); code != http.StatusNotFound {
			t.Errorf("globex %s %s = %d %s, want 404: the row is not globex's", door.method, door.path, code, body)
		}
	}
	if code, body := do(t, cfg, globex, http.MethodGet, globexHost, proposalsPath, ""); code != http.StatusOK ||
		!strings.Contains(body, `"total":0`) || strings.Contains(body, id) {
		t.Errorf("globex GET %s = %d %s, want an empty list", proposalsPath, code, body)
	}

	// Acme's row is untouched: still proposed, still at its first revision, and
	// Globex's site never took the title.
	code, body = do(t, cfg, admin, http.MethodGet, acmeHost, proposalsPath+"/"+id, "")
	if code != http.StatusOK {
		t.Fatalf("acme GET %s/%s = %d %s", proposalsPath, id, code, body)
	}
	if got := field(t, body, "state"); got != "proposed" {
		t.Errorf("acme's proposal is %q after globex's attempts, want proposed", got)
	}
	if !strings.Contains(body, `"revision":`+strconv.Itoa(1)+`}`) {
		t.Errorf("acme's proposal moved past revision 1 after globex's attempts: %s", body)
	}
	if code, body := do(t, cfg, globex, http.MethodGet, globexHost, settingsPath, ""); code != http.StatusOK ||
		strings.Contains(body, "Acme, only Acme decides") {
		t.Errorf("globex GET %s = %d %s, want globex's own settings", settingsPath, code, body)
	}
}
