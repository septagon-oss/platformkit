package httpx_test

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

// TestABearerKeyDoorAdmitsAKeyRefusesASessionAndChallengesNobody pins the three
// answers of the key-only declaration: a key of any scope is served, a session is
// a 403 that never reaches the handler, and a caller with no recognised key is a
// 401 whose challenge names the one scheme the door accepts.
func TestABearerKeyDoorAdmitsAKeyRefusesASessionAndChallengesNobody(t *testing.T) {
	var reached atomic.Int32
	api, router, f := setup(t)
	httpx.Register(api.Surfaces(probe).App, huma.Operation{
		OperationID: "own-key", Method: http.MethodGet, Path: "/own-key",
	}, httpx.BearerKey(), func(context.Context, *struct{}) (*body, error) {
		reached.Add(1)
		return &body{}, nil
	})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted route does not declare itself: %v", err)
	}
	f.allow = true

	f.principal = &tenancy.Principal{UserID: uuid.New(), TokenID: uuid.New(), Permissions: []string{}}
	if res := get(t, router, at(api, "/own-key")); res.Code != http.StatusOK || reached.Load() != 1 {
		t.Errorf("a key holding no scope = %d %s after %d handler runs, want 200 after one", res.Code, res.Body.String(), reached.Load())
	}

	reached.Store(0)
	f.signedIn()
	session := get(t, router, at(api, "/own-key"))
	if session.Code != http.StatusForbidden || !strings.Contains(session.Body.String(), httpx.CodeDenied) || reached.Load() != 0 {
		t.Errorf("a session = %d %s after %d handler runs, want 403 %s and none", session.Code, session.Body.String(), reached.Load(), httpx.CodeDenied)
	}

	f.principal = nil
	nobody := get(t, router, at(api, "/own-key"))
	if nobody.Code != http.StatusUnauthorized || !strings.Contains(nobody.Body.String(), httpx.CodeAnonymous) || reached.Load() != 0 {
		t.Errorf("nobody = %d %s after %d handler runs, want 401 %s and none", nobody.Code, nobody.Body.String(), reached.Load(), httpx.CodeAnonymous)
	}
	if got := nobody.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Errorf("nobody was challenged with %q, want %q", got, "Bearer")
	}
}
