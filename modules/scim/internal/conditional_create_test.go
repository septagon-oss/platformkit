package internal_test

// The door refuses a condition it cannot evaluate at every address that writes,
// including the two creates. A create is where a connector that believes in resource
// versions sends one — nothing exists yet to match — and an answer of 201 there would
// leave that belief intact while nothing had been compared. The case beside it,
// TestConditionalUserWritesCannotOverwriteANewerPerson, covers the addresses that name
// a resource; this one covers the two that do not, and asks the same question of the
// database: a refusal that wrote a row is not a refusal.
//
// Each half runs the same document twice, with the header as its only difference: the
// conditioned one refused and writing nothing, the plain one accepted. Without the
// second the first could pass on a bad body, a wrong address or a refused credential,
// which are all answers this door also gives and none of them the one being claimed.

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
)

func TestAConditionalCreateIsRefusedRatherThanPerformed(t *testing.T) {
	for _, tc := range []struct{ path, body, table string }{
		{"/scim/v2/Users", `{"userName":"person@acme.test","displayName":"Original"}`, "users"},
		{"/scim/v2/Groups", `{"displayName":"People"}`, "scim_groups"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			w := compose(t)
			r := provisioningRouter(t, w)
			req := httptest.NewRequest("POST", "http://acme.test"+tc.path, strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer fixture")
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("If-Match", `"1999-01-01T00:00:00Z"`)
			res := httptest.NewRecorder()
			r.ServeHTTP(res, req)
			if res.Code < 400 || res.Code >= 500 {
				t.Fatalf("conditional create: %d %s; want a refusal", res.Code, res.Body)
			}
			w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				var rows int64
				if err := tx.DB().Table(tc.table).Count(&rows).Error; err != nil {
					return err
				}
				if rows != 0 {
					t.Errorf("refused conditional create committed %d row(s) of %s", rows, tc.table)
				}
				if names := outbox(t, tx); len(names) != 0 {
					t.Errorf("refused conditional create emitted %v", names)
				}
				return nil
			})
			if plain := provisioningRequest(r, "POST", tc.path, tc.body); plain.Code >= 400 {
				t.Errorf("the same document with no condition: %d %s; want the create accepted, so that the refusal above is about the header",
					plain.Code, plain.Body)
			}
		})
	}
}
