package transport_test

// The premise every tenant boundary in this package rests on, written down once
// and tested: `Subject` and `Filter` concatenate an event name into a NATS
// subject without escaping it, so the only thing between a manifest's name and a
// wildcard in every consumer's filter is the grammar in `ValidName`.
//
// Why that is load-bearing rather than tidy: `Filter` leaves the tenant token as
// a `*` on purpose (Filters says why), and the delivery-time check
// (`AddressMismatch`) exists because of it — the routing fixes the module and
// event halves and cannot fix the tenant. Both halves of that argument are true
// only while a name is exactly the tokens it is spelled with: one `*`, which is
// the tenant and nothing else, and one token per dot. Relax the grammar by one
// alternative — allow `*`, allow `>`, allow a trailing dot — and a manifest name
// like `task.>` becomes a subscription to every event in every tenant, with
// `AddressMismatch` nodding at each self-consistent document that arrives. No
// other case in this directory would notice: they all build their own names.
//
// The matcher below is five lines of NATS token arithmetic rather than a call
// into the SDK, because nats.go v1.54.0 exports no subject matcher at all
// (`grep -rn 'func Match' $(go env GOMODCACHE)/github.com/nats-io/nats.go@v1.54.0/*.go`
// is empty), and the arithmetic it copies is the one this file's own fixtures
// depend on: `*` matches exactly one token, nothing spans two. What the broker
// itself decides is proved by the live cases in kit/events, which put messages
// on a real stream and read them back.

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/events/transport"
)

// matchesSubject is NATS' own rule for a wildcard filter: split on dots, `*`
// answers exactly one token, any other token has to equal it, and a filter that
// runs out of tokens while the subject has not (or the other way round) does not
// match. `>` is included because a name that reached it would be a name this
// file is asking whether the grammar can produce.
func matchesSubject(filter, subject string) bool {
	ft, st := strings.Split(filter, "."), strings.Split(subject, ".")
	if len(ft) != len(st) {
		return false
	}
	for i, f := range ft {
		if f == "*" {
			continue
		}
		if f == ">" || f != st[i] {
			return false
		}
	}
	return true
}

// TestANameAManifestMayEmitFixesEveryTokenOfItsAddressButTheTenant walks the
// boundary of the grammar and the consequence at the address: every name the
// grammar admits produces a filter whose only wildcard is the tenant position,
// which answers that event in every tenant and no other event in any tenant.
func TestANameAManifestMayEmitFixesEveryTokenOfItsAddressButTheTenant(t *testing.T) {
	tenant, other := uuid.New(), uuid.New()

	// Names on the accepted side of the grammar, including the three-token
	// spelling kit/rest's Spec writes ("<module>.<entity>.<verb>"), which is why
	// the token count of a name is not assumed to be two anywhere below.
	for _, name := range []string{
		"notes.created",
		"task.task.created",
		"billing.invoice_voided.posted",
		"platformkit.event_replayed",
		"audit_v2.record.purged",
	} {
		if !transport.ValidName(name) {
			t.Fatalf("transport.ValidName(%q) = false, so this case asks about a name no manifest can carry", name)
		}
		filter, subject := transport.Filter(name), transport.Subject(tenant, name)

		// The filter is this prefix, the tenant wildcard, then the name: one
		// wildcard, in the tenant's position, and nowhere else.
		if got := strings.Count(filter, "*"); got != 1 {
			t.Errorf("transport.Filter(%q) = %q with %d wildcards: the tenant is the only token a subscription may leave open", name, filter, got)
		}
		if toks := strings.Split(filter, "."); len(toks) != strings.Count(name, ".")+3 || toks[1] != "*" {
			t.Errorf("transport.Filter(%q) = %q, want %q — the prefix, the tenant token, then every token of the name", name, filter, "platformkit.*."+name)
		}
		if !matchesSubject(filter, subject) {
			t.Errorf("the filter %q does not answer the address %q it was made from", filter, subject)
		}

		// And it answers that address in every tenant, which is what the one
		// wildcard is for: one durable per (module, event) hearing every
		// tenant's delivery of one event, and no other event at all.
		for _, tenantID := range []uuid.UUID{uuid.Nil, tenant, other} {
			if !matchesSubject(filter, transport.Subject(tenantID, name)) {
				t.Errorf("the filter %q does not answer %s, which is %s in tenant %s",
					filter, transport.Subject(tenantID, name), name, tenantID)
			}
		}

		// Every event that is not this one — same module, another event of the
		// same module, one token more or one token less, another module's
		// namespace, another program's — stays outside the filter, in this
		// tenant and in every other. This is the half the grammar buys.
		parts := strings.Split(name, ".")
		module := parts[0]
		shorter := strings.Join(parts[:len(parts)-1], ".")
		for _, otherName := range []string{
			module + ".created",
			name + ".replayed",
			shorter,
			module + ".other_thing",
		} {
			for _, tenantID := range []uuid.UUID{tenant, other} {
				arrived := transport.Subject(tenantID, otherName)
				if otherName == name || !transport.ValidName(otherName) {
					continue // the event itself, or a name no manifest could carry
				}
				if matchesSubject(filter, arrived) {
					t.Errorf("the filter %q answers %q, which is %s and not %s", filter, arrived, otherName, name)
				}
			}
		}
		for _, foreign := range []string{
			transport.SubjectPrefix + "." + other.String() + "." + module,
			"acme." + other.String() + "." + name,
		} {
			if matchesSubject(filter, foreign) {
				t.Errorf("the filter %q answers the foreign address %q", filter, foreign)
			}
		}

		// The address the check accepts is the address the filter answers, and
		// the two are one token apart by construction: the tenant.
		if strings.Count(subject, ".") != strings.Count(filter, ".") {
			t.Errorf("subject %q and filter %q differ in token count, so the wildcard is not the only difference", subject, filter)
		}
	}
}

// TestNoNameTheGrammarAcceptsCarriesABrokerToken is the refusal side of the same
// boundary: the characters that mean something to a broker, and the shapes that
// would add or remove a token, are not names. Each one is asked twice — as a
// name, and as what its filter would have been — because the second is the
// damage: `platformkit.>.task.>` subscribed the world from one manifest line.
func TestNoNameTheGrammarAcceptsCarriesABrokerToken(t *testing.T) {
	for _, name := range []string{
		"notes.*", "*.created", "*", ">", "notes.>", "notes.*.created",
		"notes.**", "notes.", ".notes", "notes..created", "notes .created",
		"notes.created extra", "notes.created,other", "platformkit.>.task",
		"notes.crea>ted", "notes.crea>ted.more",
	} {
		if transport.ValidName(name) {
			t.Errorf("transport.ValidName(%q) = true: a name a broker would read as something other than itself", name)
		}
	}
}

// TestTheAddressFunctionsEscapeNothing states why the refusal above is the only
// lid there is: Subject and Filter concatenate, and neither quotes, escapes nor
// refuses. That is right — an address function that started sanitising names
// would be a second grammar beside ValidName, disagreeing with it in silence —
// and it is why the case above is not decoration. Pinned as bytes rather than
// described, because "it does not escape" is exactly the kind of sentence a
// later edit flips without noticing.
func TestTheAddressFunctionsEscapeNothing(t *testing.T) {
	tenant := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	if got, want := transport.Filter("notes.>"), "platformkit.*.notes.>"; got != want {
		t.Errorf("transport.Filter(\"notes.>\") = %q, want %q: a wildcard a name carried would reach a filter verbatim", got, want)
	}
	if got, want := transport.Subject(tenant, "notes.*.created"), "platformkit."+tenant.String()+".notes.*.created"; got != want {
		t.Errorf("transport.Subject(…, \"notes.*.created\") = %q, want %q", got, want)
	}
}

// TestTheTenantTokenIsOneToken keeps the other half of the concatenation safe:
// the segment between the prefix and the name is a UUID, so it can never be two
// tokens, an empty token or a wildcard — which is what makes one tenant's
// address unable to be another tenant's prefix and `Filter`'s single `*` unable
// to answer a message with no tenant in its address at all.
func TestTheTenantTokenIsOneToken(t *testing.T) {
	for _, tenant := range []uuid.UUID{uuid.Nil, uuid.New(), uuid.New()} {
		token := tenant.String()
		if strings.ContainsAny(token, ".*>") || token == "" {
			t.Fatalf("tenant id %q is not one broker token, so no address built from it is exact", token)
		}
		if got := strings.Count(transport.Subject(tenant, "notes.created"), "."); got != 3 {
			t.Errorf("transport.Subject(%s, \"notes.created\") = %q, which is not the four tokens an address has", tenant, transport.Subject(tenant, "notes.created"))
		}
	}
}
