package rest_test

// Every refusal in Spec.check is a panic at the mount site, which is only a useful
// rule if the mount that refused left nothing behind: a resource registered under a
// guard nobody agreed to, or an operation recorded and therefore cross-checked
// against the module manifest at boot, would be a half-mounted resource the next
// request finds. So the API a refusal walks away from is asked here for the resource
// and the operations the refused declaration would have added.

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
)

func TestARefusedMountRegistersNoResourceAndOpensNoRoute(t *testing.T) {
	for _, refusal := range []struct {
		name     string
		declare  func(s *rest.Spec[*Task])
		sentence string
	}{
		{
			name:     "one guard stated in two fields",
			declare:  func(s *rest.Spec[*Task]) { s.ReadAuth = httpx.Permission("task:read") },
			sentence: "declares ReadAuth and the permission",
		},
		{
			name:     "a public read of a tenant's rows",
			declare:  func(s *rest.Spec[*Task]) { s.Read, s.ReadAuth = "", httpx.Public() },
			sentence: "ReadAuth is public",
		},
		{
			name:     "a write guard that is no grant",
			declare:  func(s *rest.Spec[*Task]) { s.Write, s.WriteAuth = "", httpx.SignedIn() },
			sentence: "mounts update or delete under a write guard that is no grant",
		},
	} {
		t.Run(refusal.name, func(t *testing.T) {
			var mounted *httpx.API
			_, router, _ := mountAs(t, spec, caller{}, func(api *httpx.API) {
				mounted = api
				broken := spec
				broken.Entity, broken.Path = "draft", "/draft"
				refusal.declare(&broken)
				defer func() {
					msg := fmt.Sprint(recover())
					if !strings.Contains(msg, refusal.sentence) {
						t.Errorf("the mount site says %q, want it to name %q", msg, refusal.sentence)
					}
				}()
				broken.Mount(api.Surfaces(broken.Module))
			})
			if mounted == nil {
				t.Fatal("the harness mounted nothing to refuse into")
			}
			for _, op := range mounted.Recorded() {
				if strings.HasPrefix(op.OperationID, "tasks-draft") {
					t.Errorf("the refused mount recorded %s, want no operation of its own", op.OperationID)
				}
			}
			for _, res := range mounted.Resources() {
				if res.Entity == "draft" {
					t.Errorf("the refused mount registered %s.%s, want the resource left unregistered", res.Module, res.Entity)
				}
			}
			for _, asked := range []string{"/api/v1/tasks/draft", "/api/v1/tasks/draft/4f6b0e0a-0000-4000-8000-000000000001"} {
				if code, _ := call(t, router, http.MethodGet, asked, ""); code != http.StatusNotFound {
					t.Errorf("GET %s = %d, want %d: a mount that refused opens no route", asked, code, http.StatusNotFound)
				}
			}
		})
	}
}
