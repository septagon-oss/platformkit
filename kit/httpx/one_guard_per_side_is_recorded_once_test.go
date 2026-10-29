package httpx_test

// A resource can be handed its guard two ways: as a permission with an operator
// flag, or as an httpx.Auth. Registration answers the two with one declaration per
// side and writes that answer into the value it records, so what a page, the
// catalogue and the boot gate read afterwards is the guard the routes were mounted
// under rather than something each of them derives again — and a resource that says
// nothing about its guard keeps the zero it arrived with, which every door reads as
// "nobody", instead of panicking on an empty permission.

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
)

func TestRegistrationRecordsOneGuardPerSide(t *testing.T) {
	api, _, _ := setup(t)
	for _, handed := range []struct {
		name         string
		resource     httpx.Resource
		wantRead     string
		wantWrite    string
		wantFeature  string
		wantDeclared bool
	}{
		{
			// The shape a module that names a permission registers: the flags say
			// whose grant it is, and the answer is derived once, here.
			name:         "a permission and its operator flags",
			resource:     httpx.Resource{Module: "billing", Entity: "priced", Read: "price:read", Write: "price:write", OperatorRead: true},
			wantRead:     "operator_permission price:read",
			wantWrite:    "permission price:write",
			wantDeclared: true,
		},
		{
			// The shape a guard that is not a plain permission arrives in. Nothing
			// re-wraps it on the way in, so the plan feature it carries is still
			// there on the other side of registration.
			name:         "a declared guard",
			resource:     httpx.Resource{Module: "billing", Entity: "entitled", ReadBy: httpx.Permission("price:read").Needing("pricing")},
			wantRead:     "permission price:read",
			wantWrite:    "undeclared",
			wantFeature:  "pricing",
			wantDeclared: true,
		},
		{
			name: "no guard at all",
			// The shape modules/auth registers: a resource carried for its commands,
			// which guards nothing of its own. The zero is the answer, and keeping it
			// is what lets every door in front of it refuse.
			resource:     httpx.Resource{Module: "auth", Entity: "tokens", CommandsOnly: true},
			wantRead:     "undeclared",
			wantWrite:    "undeclared",
			wantDeclared: false,
		},
	} {
		api.RegisterResource(handed.resource)
		t.Run(handed.name, func(t *testing.T) {
			res := registered(t, api, handed.resource.Entity)
			if got := res.ReadAuth().String(); got != handed.wantRead {
				t.Errorf("the registered read guard is %s, want %s", got, handed.wantRead)
			}
			if got := res.WriteAuth().String(); got != handed.wantWrite {
				t.Errorf("the registered write guard is %s, want %s", got, handed.wantWrite)
			}
			if got := res.ReadAuth().Feature(); got != handed.wantFeature {
				t.Errorf("the registered read guard needs %q, want %q", got, handed.wantFeature)
			}
			if got := res.ReadBy.Declared(); got != handed.wantDeclared {
				t.Errorf("the recorded declaration answers declared=%v, want %v: the answer is written once, into the field every reader consults",
					got, handed.wantDeclared)
			}
			// The shorthand the author wrote is still what they wrote: registration
			// answers the guard, it does not rewrite the declaration it was given.
			if res.Read != handed.resource.Read || res.OperatorRead != handed.resource.OperatorRead {
				t.Errorf("the registered shorthand is %q (operator %v), want %q (operator %v)",
					res.Read, res.OperatorRead, handed.resource.Read, handed.resource.OperatorRead)
			}
			if !handed.wantDeclared && res.Readable(context.Background()) {
				t.Error("an undeclared guard is readable, so Describe would offer a resource no door admits")
			}
		})
	}
}

// registered is the value the API kept, not the one the case handed it: the
// difference between the two is what this case is about.
func registered(t *testing.T, api *httpx.API, entity string) httpx.Resource {
	t.Helper()
	for _, r := range api.Resources() {
		if r.Entity == entity {
			return r
		}
	}
	t.Fatalf("no resource recorded for %q", entity)
	return httpx.Resource{}
}
