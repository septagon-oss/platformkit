package httpx_test

// bearer_key_door_answers_the_same_at_its_address_and_behind_its_closure_test.go
// asks the same caller of both halves of one httpx.BearerKey() declaration and
// requires one answer. A declaration reaches a caller twice: as the route at its
// address, and as the read closure a page, a dashboard card or the catalogue
// calls in process beneath whatever guard those stand under. Two files beside
// this one keep each half honest against its own expectation — the address over
// HTTP, the closure as Go — and neither sees the other move, so what this asks is
// not "is 403 right" but "did the two doors part".
//
// The caller kinds are the four a key-only declaration can meet: a key that
// holds a scope, a key that holds none, a session, which carries a user and no
// key and so leaves an operation acting on the authenticating key with no
// subject, and nobody, who is refused the anonymous way, a different answer from
// holding the wrong credential. The two halves beside this file pin six of these
// eight answers and pin the two halves with different callers — the address with
// an unscoped key, the closure with a scoped one — so this asks all eight: a
// status that moved at one door and not the other, a refusal that named the other
// code, or a run counted at a door that refused is what it reports.

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestABearerKeyDoorAnswersTheSameAtItsAddressAndBehindItsClosure mounts the read
// of one entity twice, as a route and as a registered closure, and asks both as
// each of four callers. Every caller owes one answer at both doors — the status
// and denial code named below, with the module's work run at both or at neither.
func TestABearerKeyDoorAnswersTheSameAtItsAddressAndBehindItsClosure(t *testing.T) {
	api, router, f := setup(t)
	// The Authorizer always agrees: this declaration names no permission, so a
	// refusal here can only be the declaration turning a caller away.
	f.allow = true

	behind, address := 0, 0
	httpx.Register(api.Surfaces(probe).App, huma.Operation{
		OperationID: "own-key", Method: http.MethodGet, Path: "/own-key",
	}, httpx.BearerKey(), func(context.Context, *struct{}) (*body, error) {
		address++
		return &body{}, nil
	})
	api.RegisterResource(httpx.Resource{
		Module: "billing", Entity: "own-key",
		ReadBy: httpx.BearerKey(),
		List: func(context.Context, crud.Query) ([]map[string]any, int64, error) {
			behind++
			return []map[string]any{{"row": 1}}, 1, nil
		},
	})
	var door httpx.Resource
	for _, r := range api.Resources() {
		if r.Entity == "own-key" {
			door = r
		}
	}
	if door.List == nil {
		t.Fatal("the registration supplied no read closure, so the door behind it asks nothing here")
	}
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted route does not declare itself: %v", err)
	}

	askAddress := func(caller *tenancy.Principal) doorAnswer {
		t.Helper()
		address = 0
		f.principal = caller
		res := get(t, router, at(api, "/own-key"))
		blob, _ := io.ReadAll(res.Body)
		return doorAnswer{status: res.Code, code: namedCode(string(blob)), ran: address}
	}
	askBehind := func(caller *tenancy.Principal) doorAnswer {
		t.Helper()
		behind = 0
		ctx := tenancy.WithTenant(t.Context(), f.tenant)
		if caller != nil {
			ctx = tenancy.WithPrincipal(ctx, *caller)
		}
		_, _, err := door.List(ctx, crud.Query{Limit: 10})
		if err == nil {
			return doorAnswer{status: http.StatusOK, ran: behind}
		}
		var refused *problem.Problem
		if !errors.As(err, &refused) {
			t.Fatalf("the closure behind the key-only door answered with the non-problem error %v", err)
		}
		return doorAnswer{status: refused.Status, code: namedCode(err.Error()), ran: behind}
	}

	callers := []struct {
		what  string
		gives func() *tenancy.Principal
		want  doorAnswer
	}{
		{"a key that holds a scope", func() *tenancy.Principal {
			return &tenancy.Principal{UserID: uuid.New(), TokenID: uuid.New(), Permissions: []string{"billing:read"}}
		}, doorAnswer{status: http.StatusOK}},
		{"a key that holds no scope", func() *tenancy.Principal {
			return &tenancy.Principal{UserID: uuid.New(), TokenID: uuid.New(), Permissions: []string{}}
		}, doorAnswer{status: http.StatusOK}},
		// A session and nobody are refused the same way at both halves, and the
		// codes differ: the wrong credential is not the same remedy as none.
		{"a session, which carries no key", func() *tenancy.Principal {
			return &tenancy.Principal{UserID: uuid.New()}
		}, doorAnswer{status: http.StatusForbidden, code: httpx.CodeDenied}},
		{"nobody, from whom no credential arrived", func() *tenancy.Principal {
			return nil
		}, doorAnswer{status: http.StatusUnauthorized, code: httpx.CodeAnonymous}},
	}

	for _, caller := range callers {
		atTheAddress, behindTheDoor := askAddress(caller.gives()), askBehind(caller.gives())
		if atTheAddress.status != behindTheDoor.status || atTheAddress.code != behindTheDoor.code {
			t.Errorf("%s is answered %s at the address and %s behind the closure, want one answer: one declaration guards both doors, and a caller that reaches the closure beneath another page's guard owes the answer its own address gives",
				caller.what, atTheAddress, behindTheDoor)
		}
		if atTheAddress.status != caller.want.status || atTheAddress.code != caller.want.code {
			t.Errorf("%s is answered %s at the address, want %s", caller.what, atTheAddress, caller.want)
		}
		if behindTheDoor.status != caller.want.status || behindTheDoor.code != caller.want.code {
			t.Errorf("%s is answered %s behind the closure, want %s", caller.what, behindTheDoor, caller.want)
		}
		// The module's work is either done for the caller or not done: a refusal
		// that read first would have spent the query it says it never ran.
		wantRan := caller.want.status == http.StatusOK
		if (atTheAddress.ran > 0) != wantRan {
			t.Errorf("%s ran the module's work %d time(s) at the address, want %v", caller.what, atTheAddress.ran, wantRan)
		}
		if (behindTheDoor.ran > 0) != wantRan {
			t.Errorf("%s ran the module's work %d time(s) behind the closure, want %v", caller.what, behindTheDoor.ran, wantRan)
		}
	}
}

// doorAnswer is one door's reply as its caller reads it: the status, the code a
// refusal names with it, and whether any of the module's work ran.
type doorAnswer struct {
	status int
	code   string
	ran    int
}

// namedCode is the denial code a refusal body or problem names, of the two this
// door can answer with, or "" when it names neither. The two halves are compared
// on what a caller acts on rather than on the sentence each writes, which they
// word differently on purpose.
func namedCode(detail string) string {
	for _, code := range []string{httpx.CodeDenied, httpx.CodeAnonymous} {
		if strings.Contains(detail, code) {
			return code
		}
	}
	return ""
}

// String renders a door's reply the way a caller reports one: its status, and the
// code a refusal named with it.
func (a doorAnswer) String() string {
	if a.code == "" {
		return strconv.Itoa(a.status)
	}
	return strconv.Itoa(a.status) + " naming " + a.code
}
