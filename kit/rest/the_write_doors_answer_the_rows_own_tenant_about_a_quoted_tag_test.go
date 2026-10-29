package rest_test

// The row's own tenant is the one a quoted tag has something to say to.
//
// The two cases beside this one ask a write by a foreign tenant quoting a tag and
// require the tenancy refusal with nothing about a tag in it. Both state that this
// is "not the answer the row's own tenant gets when it quotes one" and neither asks
// what that answer is, so a door that said nothing about a tag to anyone would pass
// them. This case asks both sides of that sentence at the plain doors a page sends.
// The table keeps no write count, so the owner is answered the clause patch.go and
// request.go declare for such a resource — 422, a sentence about the tag — and a
// stranger is answered whose row it is. The translated door holds the owner's half
// of that clause for a row with no count; the PATCH and DELETE of the same row by
// the tenant the row names are reached here.

import (
	"net/http"
	"strings"
	"testing"
)

func TestTheWriteDoorsAnswerTheRowsOwnTenantAboutAQuotedTag(t *testing.T) {
	_, router, admin := sharedCatalogue(t, true)
	code, body := call(t, router, http.MethodPost, "/api/v1/billing/plans", `{"name":"Team","cents":4000}`)
	if code != http.StatusCreated {
		t.Fatalf("POST the catalogue row = %d %s, want 201", code, body)
	}
	key := id(t, body)
	before := readPlan(t, admin, key)
	at := "/api/v1/billing/plans/" + key
	const patch = `{"name":"rewritten by the owner","cents":4000}`

	// The row reads back to its own tenant and sends no tag, so every tag quoted
	// below is one nobody could have read from this row.
	if code, out := askAs(t, router, host, http.MethodGet, at, ""); code != http.StatusOK {
		t.Fatalf("the row's own tenant GET = %d %s, want 200", code, out)
	}
	for _, door := range []struct{ method, body string }{{http.MethodPatch, patch}, {http.MethodDelete, ""}} {
		if c, out := askQuotedAs(t, router, host, door.method, at, door.body, `"1"`); c != http.StatusUnprocessableEntity ||
			!strings.Contains(out, "write count") {
			t.Errorf("%s by the row's own tenant quoting \"1\" = %d %s, want 422 and the clause a resource with no write count declares about a quoted tag",
				door.method, c, out)
		}
	}
	// The stranger is answered whose row it is, and reads nothing about a tag: the
	// two answers differ in the status and in what the body may say.
	if c, out := askQuotedAs(t, router, "reader.test", http.MethodPatch, at, patch, `"1"`); c == http.StatusUnprocessableEntity ||
		strings.Contains(out, "write count") {
		t.Errorf("the same PATCH by a tenant outside the row's = %d %s, want the tenancy refusal and nothing about a tag, which is not what the owner is answered", c, out)
	}
	if got := readPlan(t, admin, key); got != before {
		t.Errorf("the refused writes left %+v where the row was %+v", got, before)
	}
	for _, event := range []string{"billing.plan.updated", "billing.plan.deleted"} {
		if n := count(t, admin, event); n != 0 {
			t.Errorf("the refused writes published %d %s events, want none", n, event)
		}
	}
}
