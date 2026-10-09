package internal_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/scim/contracts"
)

func TestConditionalUserWritesCannotOverwriteANewerPerson(t *testing.T) {
	for _, tc := range []struct{ method, body string }{
		{"PUT", `{"userName":"person@acme.test","displayName":"Stale","active":true}`},
		{"PATCH", `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"replace","path":"displayName","value":"Stale"}]}`},
		{"DELETE", ""},
	} {
		t.Run(tc.method, func(t *testing.T) {
			w := compose(t)
			r := provisioningRouter(t, w)
			res := provisioningRequest(r, "POST", "/scim/v2/Users", `{"userName":"person@acme.test","displayName":"Original","active":true}`)
			if res.Code != 201 {
				t.Fatalf("create: %d %s", res.Code, res.Body)
			}
			var original contracts.UserResource
			if err := json.Unmarshal(res.Body.Bytes(), &original); err != nil {
				t.Fatal(err)
			}
			path := "/scim/v2/Users/" + original.ID.String()
			res = provisioningRequest(r, "PATCH", path, `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"replace","path":"displayName","value":"Current"}]}`)
			if res.Code != 200 {
				t.Fatalf("current write: %d %s", res.Code, res.Body)
			}
			var current contracts.UserResource
			if err := json.Unmarshal(res.Body.Bytes(), &current); err != nil {
				t.Fatal(err)
			}
			if original.Meta.Version == "" || original.Meta.Version == current.Meta.Version {
				t.Fatal("fixture must have two distinct resource versions")
			}
			var before int
			w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				before = len(outbox(t, tx))
				return nil
			})
			req := httptest.NewRequest(tc.method, "http://acme.test"+path, strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer fixture")
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("If-Match", `"`+original.Meta.Version+`"`)
			res = httptest.NewRecorder()
			r.ServeHTTP(res, req)
			// An unsupported conditional write may be refused outright. It must
			// never become an unconditional mutation of the newer resource.
			if res.Code < 400 || res.Code >= 500 {
				t.Errorf("conditional %s must refuse a stale version, got %d %s", tc.method, res.Code, res.Body)
			}
			var returned map[string]json.RawMessage
			if json.Unmarshal(res.Body.Bytes(), &returned) == nil && returned["id"] != nil {
				t.Error("refused conditional write returned a person row")
			}
			w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				got, err := w.svc.GetUser(ctx, tx, original.ID)
				if err != nil {
					return err
				}
				if got.DisplayName != "Current" || !got.Active {
					t.Errorf("conditional write changed the newer person: name=%q active=%v", got.DisplayName, got.Active)
				}
				if after := len(outbox(t, tx)); after != before {
					t.Errorf("conditional write emitted events: %d -> %d", before, after)
				}
				return nil
			})
		})
	}
}
