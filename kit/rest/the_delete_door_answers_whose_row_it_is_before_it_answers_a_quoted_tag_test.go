package rest_test

// Whose row it is gets answered before anything a quoted tag has to say.
//
// deleteRow rechecks the tenant before it honours If-Match — the order validateUpdate
// names as deliberate, that tenant ownership is checked before the tag so a stale-tag
// refusal cannot disclose a foreign row's revision. On a catalogue table that order is
// the whole of the difference between two answers: its policy lets every tenant read
// one shared list and lets only the tenant the row names write it, so the row is
// visible here and only the write is scoped. A tenant that may not write the row is
// answered the tenancy refusal, and is answered nothing about a tag — not the answer
// the row's own tenant gets when it quotes one.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
)

// askQuotedAs is askAs with a host and a condition together: quoted() sends the tag on
// the host the package fixed, askAs sends the host without a condition, and asking
// about a foreign tenant's row with a tag quoted needs both at once.
func askQuotedAs(t *testing.T, r http.Handler, as, method, path, body, ifMatch string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, "http://"+as+path, strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	req.Header.Set("Content-Type", "application/json")
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

func TestTheDeleteDoorAnswersWhoseRowItIsBeforeItAnswersAQuotedTag(t *testing.T) {
	_, router, admin := sharedCatalogue(t, true)
	code, body := call(t, router, http.MethodPost, "/api/v1/billing/plans", `{"name":"Team","cents":4000}`)
	if code != http.StatusCreated {
		t.Fatalf("POST the catalogue row = %d %s, want 201", code, body)
	}
	key := id(t, body)
	before := readPlan(t, admin, key)
	at := "/api/v1/billing/plans/" + key

	// What that tenant is answered on this row when it quotes nothing. Quoting a tag
	// may not change it: the tenancy refusal comes first, so a tag says nothing.
	plain, _ := askAs(t, router, "reader.test", http.MethodDelete, at, "")
	if plain != http.StatusNotFound {
		t.Fatalf("DELETE by a tenant that may not write the row, quoting nothing = %d, want 404: the row's tenant is asked before any tag", plain)
	}
	if code, out := askQuotedAs(t, router, "reader.test", http.MethodDelete, at, "", `"1"`); code != plain ||
		strings.Contains(out, "write count") || strings.Contains(out, "revision") {
		t.Fatalf("the same DELETE quoting \"1\" = %d %s, want %d and nothing about a tag, a write count or a revision: whose row it is is answered first",
			code, out, plain)
	}
	if got := readPlan(t, admin, key); got != before {
		t.Errorf("the refused delete left %+v where the row was %+v", got, before)
	}
	if n := count(t, admin, "billing.plan.deleted"); n != 0 {
		t.Errorf("the refused delete published %d billing.plan.deleted events, want none", n)
	}
}
