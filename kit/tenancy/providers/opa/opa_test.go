package opa_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/kit/tenancy/providers/opa"
)

const owned = `package test.owned

decision := {"allow": true, "reason": "owner"} if input.resource.attributes.owner == input.actor.id

decision := {"allow": false, "reason": "not the owner"} if {
	input.action == "doc:edit"
	input.resource.attributes.owner != input.actor.id
}
`

func request(action, actor, owner string) tenancy.PolicyRequest {
	id := uuid.New()
	return tenancy.PolicyRequest{
		Tenant: tenancy.Tenant{ID: id, Slug: "acme"}, Actor: tenancy.PolicyActor{Kind: tenancy.PolicyUser, ID: actor},
		Action: action, Resource: tenancy.PolicyResource{TenantID: id, Kind: "doc", ID: uuid.NewString(),
			Attributes: map[string]any{"owner": owner}},
	}
}

// TestADecisionIsTheRulesAnswerAndSilenceIsARefusal: the input carries the actor and the
// resource's attributes as the module sent them, an object answer is taken as it is, and a
// request no rule speaks to is refused — never allowed because nothing said no.
func TestADecisionIsTheRulesAnswerAndSilenceIsARefusal(t *testing.T) {
	p, err := opa.New(t.Context(), "owned.rego", "test.owned", owned)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		r      tenancy.PolicyRequest
		allow  bool
		reason string
	}{
		{"owner", request("doc:edit", "ana", "ana"), true, "owner"},
		{"stranger", request("doc:edit", "bo", "ana"), false, "not the owner"},
		{"undefined", request("doc:delete", "bo", "ana"), false, "no rule allows doc:delete"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := p.Decide(t.Context(), tc.r)
			if err != nil {
				t.Fatal(err)
			}
			if d.Allowed != tc.allow || d.Reason != tc.reason || d.Revision != p.Revision() {
				t.Fatalf("decision = %+v, want allow=%v reason=%q revision %s", d, tc.allow, tc.reason, p.Revision())
			}
		})
	}
}

// TestAnAnswerThatIsNotADecisionIsAnOutage: a rule that answers a bare true, or an object
// without a boolean allow, or conflicts with itself, is a defect in the policy. Reading any
// of them as an allow would let a typo open a door, so each is ErrPolicyUnavailable, which
// RequirePolicy answers with a refusal.
func TestAnAnswerThatIsNotADecisionIsAnOutage(t *testing.T) {
	for name, source := range map[string]string{
		"bare boolean": "package test.bad\n\ndecision := true\n",
		"string allow": "package test.bad\n\ndecision := {\"allow\": \"yes\"}\n",
		"conflict":     "package test.bad\n\ndecision := {\"allow\": true} if input.action\ndecision := {\"allow\": false} if input.action\n",
	} {
		t.Run(name, func(t *testing.T) {
			p, err := opa.New(t.Context(), "bad.rego", "test.bad", source)
			if err != nil {
				t.Fatal(err)
			}
			d, err := p.Decide(t.Context(), request("doc:edit", "ana", "ana"))
			if !errors.Is(err, tenancy.ErrPolicyUnavailable) || d.Allowed {
				t.Fatalf("decide = %+v, %v; want ErrPolicyUnavailable and no allow", d, err)
			}
		})
	}
}

// TestTheRevisionNamesTheSourceAndACompileErrorIsRefusedAtNew: two compilations of the
// same rules carry one revision and a one-character change carries another, so a
// recorded decision names the exact rules that made it; and a module that does not
// compile is an error at New, before any request can reach it.
func TestTheRevisionNamesTheSourceAndACompileErrorIsRefusedAtNew(t *testing.T) {
	a := opa.MustNew("owned.rego", "test.owned", owned)
	b := opa.MustNew("owned.rego", "test.owned", owned)
	c := opa.MustNew("owned.rego", "test.owned", owned+"\n")
	if a.Revision() != b.Revision() || a.Revision() == c.Revision() || len(a.Revision()) != len("sha256:")+12 {
		t.Fatalf("revisions %s %s %s: want equal for equal sources, different otherwise", a.Revision(), b.Revision(), c.Revision())
	}
	if _, err := opa.New(t.Context(), "broken.rego", "test.broken", "package test.broken\n\ndecision := {"); err == nil {
		t.Fatal("a module that does not parse compiled")
	}
	if _, err := opa.New(t.Context(), "owned.rego", " ", owned); err == nil {
		t.Fatal("a policy with no package compiled")
	}
}
