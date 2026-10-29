package httpx_test

// a_key_only_write_door_refuses_a_session_behind_its_closure_as_it_does_at_its_address_test.go
// asks the write of one httpx.BearerKey() declaration at both of its halves and
// requires one answer. The family beside this file asks reads: the address of a
// declared read, its closure, and now the same caller of both. A write reaches a
// caller the same two ways — a generated form's save and a dashboard card call
// `Resource.Update` and `Resource.Delete` in process, beneath whatever guard that
// page stands under — and every case in this package that registers a write door
// declares it `SignedIn()`, so a key-only write is guarded only by the middleware
// at its address. A session would then write through the page it is reading, at a
// door whose own address turns that caller away.
//
// DELETE is the verb asked here because it is the one write that arrives with no
// body, so both halves are asked the same request rather than one with a payload
// and one without.

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestAKeyOnlyWriteDoorRefusesASessionBehindItsClosureAsItDoesAtItsAddress mounts
// the write of one key-only entity as a route and as a registered closure, and asks
// both as each of the four callers that declaration can meet. Each owes one status,
// one denial code and one answer about whether the write happened.
func TestAKeyOnlyWriteDoorRefusesASessionBehindItsClosureAsItDoesAtItsAddress(t *testing.T) {
	api, router, f := setup(t)
	// The Authorizer always agrees: this declaration names no permission, so a
	// refusal here can only be the declaration turning a caller away.
	f.allow = true

	behind, address := 0, 0
	httpx.Register(api.Surfaces(probe).App, huma.Operation{
		OperationID: "own-key-remove", Method: http.MethodDelete, Path: "/own-key-remove",
	}, httpx.BearerKey(), func(context.Context, *struct{}) (*body, error) {
		address++
		return &body{}, nil
	})
	api.RegisterResource(httpx.Resource{
		Module: "billing", Entity: "own-key-remove",
		WriteBy: httpx.BearerKey(),
		Delete: func(context.Context, uuid.UUID) error {
			behind++
			return nil
		},
	})
	var door httpx.Resource
	for _, r := range api.Resources() {
		if r.Entity == "own-key-remove" {
			door = r
		}
	}
	if door.Delete == nil {
		t.Fatal("the registration supplied no write closure, so the door behind it asks nothing here")
	}
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted route does not declare itself: %v", err)
	}

	askAddress := func(caller *tenancy.Principal) writeAnswer {
		t.Helper()
		address = 0
		f.principal = caller
		res := request(t, router, http.MethodDelete, at(api, "/own-key-remove"), host)
		blob, _ := io.ReadAll(res.Body)
		return writeAnswer{status: res.Code, code: denialCode(string(blob)), wrote: address > 0}
	}
	askBehind := func(caller *tenancy.Principal) writeAnswer {
		t.Helper()
		behind = 0
		ctx := tenancy.WithTenant(t.Context(), f.tenant)
		if caller != nil {
			ctx = tenancy.WithPrincipal(ctx, *caller)
		}
		err := door.Delete(ctx, uuid.New())
		if err == nil {
			return writeAnswer{status: http.StatusOK, wrote: behind > 0}
		}
		var refused *problem.Problem
		if !errors.As(err, &refused) {
			t.Fatalf("the closure behind the key-only write door answered with the non-problem error %v", err)
		}
		return writeAnswer{status: refused.Status, code: denialCode(err.Error()), wrote: behind > 0}
	}

	callers := []struct {
		what  string
		gives func() *tenancy.Principal
		want  writeAnswer
	}{
		{"a key that holds a scope", func() *tenancy.Principal {
			return &tenancy.Principal{UserID: uuid.New(), TokenID: uuid.New(), Permissions: []string{"billing:write"}}
		}, writeAnswer{status: http.StatusOK, wrote: true}},
		{"a key that holds no scope", func() *tenancy.Principal {
			return &tenancy.Principal{UserID: uuid.New(), TokenID: uuid.New(), Permissions: []string{}}
		}, writeAnswer{status: http.StatusOK, wrote: true}},
		// A session and nobody are refused the same way at both halves, and the codes
		// differ: the wrong credential is not the same remedy as none. Both are owed
		// the refusal before the write, which is what the count below asks.
		{"a session, which carries no key", func() *tenancy.Principal {
			return &tenancy.Principal{UserID: uuid.New()}
		}, writeAnswer{status: http.StatusForbidden, code: httpx.CodeDenied}},
		{"nobody, from whom no credential arrived", func() *tenancy.Principal {
			return nil
		}, writeAnswer{status: http.StatusUnauthorized, code: httpx.CodeAnonymous}},
	}

	for _, caller := range callers {
		atTheAddress, behindTheDoor := askAddress(caller.gives()), askBehind(caller.gives())
		if atTheAddress != behindTheDoor {
			t.Errorf("%s is answered %s at the address and %s behind the closure, want one answer: one declaration guards both halves of a write, and the page whose save reaches the closure owes its caller the answer that page's own address gives",
				caller.what, atTheAddress, behindTheDoor)
		}
		if atTheAddress != caller.want {
			t.Errorf("%s is answered %s at the address, want %s", caller.what, atTheAddress, caller.want)
		}
		if behindTheDoor != caller.want {
			t.Errorf("%s is answered %s behind the closure, want %s", caller.what, behindTheDoor, caller.want)
		}
	}
}

// writeAnswer is one half's reply as its caller reads it: the status, the code a
// refusal names with it, and whether any of the write ran. Compared whole, so a
// half that refused after writing reports itself as the difference it is.
type writeAnswer struct {
	status int
	code   string
	wrote  bool
}

// denialCode is the denial code a refusal body or problem names, of the two this
// door can answer with, or "" when it names neither. The halves are compared on
// what a caller acts on rather than on the sentence each writes, which they word
// differently on purpose.
func denialCode(detail string) string {
	for _, code := range []string{httpx.CodeDenied, httpx.CodeAnonymous} {
		if strings.Contains(detail, code) {
			return code
		}
	}
	return ""
}

// String renders a half's reply the way a caller reports one: whether the write
// happened, its status, and the code a refusal named with it.
func (a writeAnswer) String() string {
	what := "wrote"
	if !a.wrote {
		what = "wrote nothing"
	}
	if a.code == "" {
		return what + " at " + http.StatusText(a.status)
	}
	return what + " at " + http.StatusText(a.status) + " naming " + a.code
}
