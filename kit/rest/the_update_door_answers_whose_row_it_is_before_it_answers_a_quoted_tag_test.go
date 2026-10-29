package rest_test

// The update door answers whose row it is before anything a quoted tag has to say.
//
// validateUpdate names its refusal order out loud — validation, tenant ownership,
// revision, then gate, "so a stale-tag refusal cannot disclose a foreign row's
// revision". Nothing asks it. On the catalogue table the order is the whole of the
// difference between two answers: its policy lets every tenant read one shared list
// and lets only the tenant the row names write it, so the row is visible here and
// only the write is scoped. A tenant that may not write the row is answered the
// tenancy refusal, and is answered nothing about a tag — not the sentence
// refuseStaleTag prints, which is about the row's write count and is a sentence a
// request outside the row's tenant has no business reading.

import (
	"net/http"
	"strings"
	"testing"
)

func TestTheUpdateDoorAnswersWhoseRowItIsBeforeItAnswersAQuotedTag(t *testing.T) {
	_, router, admin := sharedCatalogue(t, true)
	code, body := call(t, router, http.MethodPost, "/api/v1/billing/plans", `{"name":"Team","cents":4000}`)
	if code != http.StatusCreated {
		t.Fatalf("POST the catalogue row = %d %s, want 201", code, body)
	}
	key := id(t, body)
	before := readPlan(t, admin, key)
	at := "/api/v1/billing/plans/" + key
	const patch = `{"name":"rewritten across","cents":4000}`

	// The row is readable by the tenant asking about it, so every refusal below is
	// about writing a row it may see and not about an address nobody can reach.
	if code, out := askAs(t, router, "reader.test", http.MethodGet, at, ""); code != http.StatusOK ||
		!strings.Contains(out, "Team") {
		t.Fatalf("reader tenant GET = %d %s, want 200 with the shared row in it", code, out)
	}

	// What that tenant is answered when it quotes nothing. Quoting a tag may not
	// change the status, nor add a word about the tag.
	plain, _ := askAs(t, router, "reader.test", http.MethodPatch, at, patch)
	if plain != http.StatusNotFound {
		t.Fatalf("PATCH by a tenant that may not write the row, quoting nothing = %d, want 404: the row's tenant is asked before any tag", plain)
	}
	for _, tag := range []string{`"1"`, `"bogus"`} {
		if code, out := askQuotedAs(t, router, "reader.test", http.MethodPatch, at, patch, tag); code != plain ||
			strings.Contains(out, "write count") || strings.Contains(out, "revision") ||
			strings.Contains(out, "tag") {
			t.Fatalf("the same PATCH quoting %s = %d %s, want %d and nothing about a tag, a write count or a revision: whose row it is is answered first",
				tag, code, out, plain)
		}
	}
	if got := readPlan(t, admin, key); got != before {
		t.Errorf("the refused patches left %+v where the row was %+v", got, before)
	}
	if n := count(t, admin, "billing.plan.updated"); n != 0 {
		t.Errorf("the refused patches published %d billing.plan.updated events, want none", n)
	}
}
