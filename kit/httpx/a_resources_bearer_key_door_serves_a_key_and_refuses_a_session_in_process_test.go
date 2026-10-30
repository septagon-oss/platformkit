package httpx_test

// a_resources_bearer_key_door_serves_a_key_and_refuses_a_session_in_process_test.go
// asks the one declaration no in-process door asks yet. A resource holds its read
// closure behind what it declares, and a page, a dashboard card and the catalogue all
// call that closure beneath whatever route they happen to stand in front of, so the
// answer behind the door has to be the answer the route gives. Two files beside this
// one ask the door that names a permission and the doors that name none — signed-in
// and any-credential. The key-only declaration is asked there only at the address;
// behind the closure it answered nothing.
//
// The gap matters most for this kind. A key is the credential of a caller that is not a
// browser, and a key-only operation is about that key: it acts on the principal that
// authenticated the request, which a session never carries. A closure that admitted a
// session would hand a browser caller an operation whose whole subject is a key nobody
// held out, while its own address turned that caller away — the same ceiling read only
// where somebody remembered to read it. So each caller here owes an answer and a run
// count: the key is served once, and both refusals — the session that carries no key
// and the caller nobody recognised — run nothing and ask the Authorizer nothing,
// because no grant was ever the question.

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestAResourcesBearerKeyDoorServesAKeyAndRefusesASessionInProcess registers a resource
// whose read is declared httpx.BearerKey() and calls its List closure in process as
// three callers inside the tenant the host would have resolved to: a key, a session,
// and nobody. The key is served once. A session owes a refusal naming the denial code
// — the answer the same declaration gives at its address — and an unrecognised request
// owes a 401 naming the anonymous code. Each refusal runs the closure not at all and
// asks the Authorizer nothing.
func TestAResourcesBearerKeyDoorServesAKeyAndRefusesASessionInProcess(t *testing.T) {
	api, _, f := setup(t)
	// An Authorizer that always agrees keeps a refusal from being mistaken for a
	// denial of some permission: this declaration names none.
	f.allow = true

	ran := 0
	api.RegisterResource(httpx.Resource{
		Module: "billing", Entity: "own-key",
		ReadBy: httpx.BearerKey(),
		List: func(context.Context, crud.Query) ([]map[string]any, int64, error) {
			ran++
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
		t.Fatal("the registration supplied no read closure behind the key-only declaration, so this asks nothing")
	}

	ask := func(p *tenancy.Principal) (int, []map[string]any, int64, error) {
		t.Helper()
		ran = 0
		f.asked.Store(0)
		ctx := tenancy.WithTenant(t.Context(), f.tenant)
		if p != nil {
			ctx = tenancy.WithPrincipal(ctx, *p)
		}
		rows, total, err := door.List(ctx, crud.Query{Limit: 10})
		return ran, rows, total, err
	}

	// The subject of the operation: a key, whatever scope it holds of its own.
	runs, rows, total, err := ask(&tenancy.Principal{UserID: uuid.New(), TokenID: uuid.New(), Permissions: []string{"billing:read"}})
	if err != nil || runs != 1 || len(rows) != 1 || total != 1 {
		t.Errorf("a key through the key-only door's List = %v after %d run(s), %d row(s) and a total of %d, want nil after one run, one row and one: the key is the caller this operation is about",
			err, runs, len(rows), total)
	}
	if err == nil && f.asked.Load() != 0 {
		t.Errorf("the key-only door asked the Authorizer %d time(s) about a caller it served, want never: the declaration names no permission, so no grant was ever in question",
			f.asked.Load())
	}

	// A session carries no key, so this operation has no subject to act on. The
	// address answers 403 naming the denial; the closure owes the same answer, and
	// owes it without reading anything.
	runs, rows, total, err = ask(&tenancy.Principal{UserID: uuid.New()})
	assertRefused(t, "a session through the key-only door's List", err, http.StatusForbidden, httpx.CodeDenied)
	if runs != 0 || len(rows) != 0 || total != 0 {
		t.Errorf("that session was answered with %d row(s) and a total of %d after %d run(s), want nothing and no work: a refusal of an operation with no subject is owed before its read",
			len(rows), total, runs)
	}
	if got := f.asked.Load(); got != 0 {
		t.Errorf("the key-only door asked the Authorizer %d time(s) about a caller it refused, want never", got)
	}

	// And nobody at all is refused the anonymous way, in process as at the address:
	// no credential arrived, which is a different answer from holding the wrong one.
	runs, rows, total, err = ask(nil)
	assertRefused(t, "an unrecognised request through the key-only door's List", err, http.StatusUnauthorized, httpx.CodeAnonymous)
	if runs != 0 || len(rows) != 0 || total != 0 {
		t.Errorf("that unrecognised request was answered with %d row(s) and a total of %d after %d run(s), want nothing and no work", len(rows), total, runs)
	}
}

// assertRefused reports an error that is not a refusal of this status naming this code.
// Status and code are asked together because a door that answered the right status with
// another sentence would send the caller to the wrong remedy.
func assertRefused(t *testing.T, what string, err error, status int, code string) {
	t.Helper()
	if err == nil {
		t.Errorf("%s = no error, want a %d naming %s", what, status, code)
		return
	}
	var refused *problem.Problem
	if !errors.As(err, &refused) {
		t.Errorf("%s = the non-problem error %v, want a %d naming %s", what, err, status, code)
		return
	}
	if refused.Status != status || !strings.Contains(err.Error(), code) {
		t.Errorf("%s = %v, want %d naming %s", what, err, status, code)
	}
}
