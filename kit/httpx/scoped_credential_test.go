package httpx_test

// The ceiling a scoped credential carries is enforced by this package, not by
// whichever module minted the credential, so this is where the rule is pinned.
//
// tenancy.Principal.Permissions says "a non-nil list is a ceiling, not an
// addition", and httpx.Authorizer says the operation's declaration is the
// question asked of the authorizer. The two halves meet at one shape, which is
// the commonest one in a real application: an operation declared
// httpx.SignedIn(), about the caller themselves, naming no permission. Such an
// operation asks the caller for their whole authority — being the caller is all
// it checks, and what sits behind it resolves the holder's roles — so a
// credential narrowed to a list may not stand in for it. If the ceiling is read
// only after that kind has served the request, a key scoped to one read reaches
// every self-service door there is at its holder's full authority: it mints a
// key wider than itself, replaces the person's recovery codes and hands over the
// new set, ends the person's sessions. That is the narrowed credential widening
// itself back to the holder, which is the escalation the field says cannot
// happen.
//
// Each case below therefore asks two questions: the status a refusal has to
// carry, and whether the handler ran. A refusal that reached the handler wrote
// something, and a refusal that asked the authorizer asked a question about the
// holder rather than about the caller.

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// scopedAPI mounts four probes over one fixture: a door about the caller
// themselves, which names no permission; a door this key's own scope names; a
// door it does not; and a door that names no permission and spends none of the
// caller's authority either — the one an AnyCredential declaration is for.
// reached counts the handler bodies that ran.
func scopedAPI(t *testing.T, reached *atomic.Int32) (*httpx.API, http.Handler, *fixture) {
	t.Helper()
	api, router, f := setup(t)
	ask := func(id, path string, auth httpx.Auth) {
		httpx.Register(api.Surfaces(probe).App, huma.Operation{
			OperationID: id, Method: http.MethodGet, Path: path,
		}, auth, func(context.Context, *struct{}) (*body, error) {
			reached.Add(1)
			return &body{}, nil
		})
	}
	ask("scoped-self", "/self", httpx.SignedIn())
	ask("scoped-read", "/reads", httpx.Permission("billing:read"))
	ask("scoped-write", "/writes", httpx.Permission("billing:write"))
	ask("scoped-describe", "/describes", httpx.AnyCredential())
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted routes do not declare themselves: %v", err)
	}
	return api, router, f
}

func TestAScopedCredentialIsRefusedEveryOperationThatNamesNoPermission(t *testing.T) {
	var reached atomic.Int32
	api, router, f := scopedAPI(t, &reached)
	f.allow = true
	f.principal = &tenancy.Principal{
		UserID: uuid.New(), Permissions: []string{"billing:read"},
	}

	// The door about the caller. Refused, before the authorizer is asked and
	// before the handler runs, so the refusal writes nothing and says nothing
	// about the holder's roles.
	self := get(t, router, at(api, "/self"))
	if self.Code != http.StatusForbidden {
		t.Errorf("a key scoped to billing:read at an operation declaring SignedIn = %d %s, want 403 — the operation spends its caller's whole authority, which is what a scope exists to stop",
			self.Code, self.Body.String())
	}
	if !strings.Contains(self.Body.String(), httpx.CodeDenied) {
		t.Errorf("the refusal does not carry its code: %s", self.Body.String())
	}
	if asked := f.asked.Load(); asked != 0 {
		t.Errorf("the refusal asked the authorizer %d questions, want none: a scoped credential is decided by what it carries, not by what its holder holds", asked)
	}
	if got := reached.Load(); got != 0 {
		t.Errorf("the refusal reached the handler %d times, want never: a refusal that ran wrote something", got)
	}

	// The same credential at the door its own scope names. Without this the case
	// above could only have been satisfied by refusing every scoped request, and
	// a key that opens nothing is not a credential.
	if read := get(t, router, at(api, "/reads")); read.Code != http.StatusOK {
		t.Errorf("the same key at the operation its scope names = %d %s, want 200", read.Code, read.Body.String())
	}
	// And at the door it does not name, which is the older half of the same rule.
	if write := get(t, router, at(api, "/writes")); write.Code != http.StatusForbidden {
		t.Errorf("the same key at an operation naming a permission it does not carry = %d, want 403", write.Code)
	}
}

// TestASessionCallerReachesAnOperationThatNamesNoPermission is the control: the
// refusal above is about the credential that carries its own list, and not about
// the operation. The same principal, with nil Permissions — which is what a
// session cookie resolves to — is the caller the operation was written for.
func TestASessionCallerReachesAnOperationThatNamesNoPermission(t *testing.T) {
	var reached atomic.Int32
	api, router, f := scopedAPI(t, &reached)
	f.allow = true
	f.signedIn()
	if f.principal == nil || f.principal.Permissions != nil {
		t.Fatal("the fixture's signed-in caller carries a permission list, which is the thing this case is not about")
	}

	res := get(t, router, at(api, "/self"))
	if res.Code != http.StatusOK {
		t.Fatalf("a caller whose roles decide = %d %s, want 200", res.Code, res.Body.String())
	}
	if reached.Load() == 0 {
		t.Error("the whole-authority caller never reached the handler either")
	}
}

// TestAScopedCredentialReachesAnOperationThatSpendsNoAuthorityOnItsHolder is the
// half the ceiling needs to be an argument rather than a reflex. Refusing every
// operation that names no permission would be safe and would end the bearer
// credential: the resource catalog names none — there is no permission to name —
// and it is the first request of a client that is not a browser, whose only
// credential is a key. It is admitted because it spends nothing: it answers what
// the caller may already reach, and the document is built by asking the
// authorizer about this caller, so the narrowed credential receives the narrowed
// document. The ceiling is about authority spent, not about the absence of a
// permission name.
//
// The three callers are the whole table. The key that carries its own scope is
// served, because the operation asks nothing it narrows. The session caller who
// decides by role is served, which is the control that says the refusal above is
// about the credential. And an anonymous caller is refused — this declaration
// admits any *credential*, which is not the same sentence as any caller, and a
// route that could be read that way would be Public() with an extra step.
func TestAScopedCredentialReachesAnOperationThatSpendsNoAuthorityOnItsHolder(t *testing.T) {
	var reached atomic.Int32
	api, router, f := scopedAPI(t, &reached)
	f.allow = true
	f.principal = &tenancy.Principal{
		UserID: uuid.New(), Permissions: []string{"billing:read"},
	}

	res := get(t, router, at(api, "/describes"))
	if res.Code != http.StatusOK {
		t.Errorf("a key scoped to billing:read at an operation declaring AnyCredential = %d %s, want 200: "+
			"the operation names no permission and spends none of its caller's authority, so there is "+
			"nothing here for the scope to be refused for — and this is the door a shell that is not a "+
			"browser opens at, so refusing it costs a working credential the address that names its work",
			res.Code, res.Body.String())
	}
	if got := reached.Load(); got != 1 {
		t.Errorf("the served request reached the handler %d times, want once", got)
	}

	// The session caller, and the handler runs for them too.
	reached.Store(0)
	f.signedIn()
	if res := get(t, router, at(api, "/describes")); res.Code != http.StatusOK || reached.Load() != 1 {
		t.Errorf("the same door to a caller whose roles decide = %d %s after %d handler runs, want 200 after one",
			res.Code, res.Body.String(), reached.Load())
	}

	// Nobody at all, which is the door this is not.
	reached.Store(0)
	f.principal = nil
	anon := get(t, router, at(api, "/describes"))
	if anon.Code == http.StatusOK || !strings.Contains(anon.Body.String(), httpx.CodeAnonymous) {
		t.Errorf("an anonymous caller at an operation declaring AnyCredential = %d %s, want a refusal that "+
			"names %s: any credential is not every caller, and an operation that admitted nobody would be "+
			"Public()", anon.Code, anon.Body.String(), httpx.CodeAnonymous)
	}
	if got := reached.Load(); got != 0 {
		t.Errorf("the anonymous refusal reached the handler %d times, want never", got)
	}
}
