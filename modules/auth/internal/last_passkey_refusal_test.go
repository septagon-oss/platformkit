package internal_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

func TestLastPasskeyRefusalReturnsAReasonAndKeepsState(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	session := signIn(t, router, "ada@acme.localhost")
	code, body := enrolPasskey(t, router, session, newSoftAuthenticator(t, host))
	if code != http.StatusCreated {
		t.Fatalf("enrol = %d %s", code, body)
	}
	var factor contracts.Factor
	if err := json.Unmarshal([]byte(body), &factor); err != nil {
		t.Fatal(err)
	}
	keys, withdrawn := passkeyLedger(t, conn)
	res := call(t, router, http.MethodDelete, "/api/v1/auth/factors/"+factor.ID.String(), "", withSession(session))
	if res.Code != http.StatusConflict {
		t.Errorf("withdraw last factor = %d %s, want 409", res.Code, res.Body.String())
	}
	var problem struct {
		Detail string `json:"detail"`
		ID     string `json:"id"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &problem); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(problem.Detail, "enrol another") || problem.ID != "" {
		t.Errorf("refusal = %+v, want an actionable reason and no factor row", problem)
	}
	if sessionCookie(res) != "" {
		t.Error("the refused withdrawal issued a session")
	}
	if afterKeys, afterEvents := passkeyLedger(t, conn); afterKeys != keys || afterEvents != withdrawn {
		t.Errorf("refused withdrawal changed credentials %d -> %d or events %d -> %d", keys, afterKeys, withdrawn, afterEvents)
	}
}
