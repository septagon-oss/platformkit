package transport_test

// Every address an event can be published at, and every wildcard a subscription can
// answer, used to be spelled twice: once for the deployment that names no app and once
// for the app named in configuration, two exported functions per concept with two
// documents that had to be kept in step. The pair is one function taking the name — the
// unnamed deployment passes appname.Name("") — and the unnamed form is now a delegation
// to it. These cases pin the delegation: a caller that migrates must land on the byte it
// was already publishing at, and a name that needs the older spelling still gets it.

import (
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/events/transport"
)

func TestTheNamedAddressDoorAnswersWhatTheUnnamedOneAnswered(t *testing.T) {
	tenant := uuid.MustParse("0f0f0f0f-0f0f-0f0f-0f0f-0f0f0f0f0f0f")
	const name = "billing.invoice_issued"
	acme := appname.MustParse("acme")

	// Name("") is the deployment that names no app, which is what the unlabelled
	// forms have always meant.
	none := appname.Name("")
	cases := []struct {
		what string
		got  string
		want string
	}{
		{"an unnamed address", transport.SubjectFor(none, tenant, name), transport.Subject(tenant, name)},
		{"a named address", transport.SubjectFor(acme, tenant, name), transport.AppSubject(acme, tenant, name)},
		{"an unnamed filter", transport.FilterFor(none, name), transport.Filter(name)},
		{"a named filter", transport.FilterFor(acme, name), transport.AppFilter(acme, name)},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: SubjectFor/FilterFor answered %q, the older door %q", c.what, c.got, c.want)
		}
	}
	if got, want := transport.FiltersFor(none, name), transport.Filters(name); !slices.Equal(got, want) {
		t.Errorf("an unnamed subscription's addresses: FiltersFor answered %v, the older door %v", got, want)
	}
	if got, want := transport.FiltersFor(acme, name), transport.AppFilters(acme, name); !slices.Equal(got, want) {
		t.Errorf("app acme's addresses: FiltersFor answered %v, the older door %v", got, want)
	}
	// The delegation is not the same string for every input: an app that names
	// itself publishes one token deeper than a deployment that names nothing, and a
	// filter has to keep saying which.
	if transport.SubjectFor(acme, tenant, name) == transport.SubjectFor(none, tenant, name) {
		t.Error("naming the app changed no address, so one app's subject carries no app")
	}
}
