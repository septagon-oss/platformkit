package internal_test

import (
	"net/url"
	"testing"

	"github.com/septagon-oss/platformkit/modules/scim/contracts"
)

func TestEveryAdvertisedSchemaCanBeReadByItsIdentifier(t *testing.T) {
	w := compose(t)
	r := provisioningRouter(t, w)
	for _, id := range []string{contracts.SchemaUser, contracts.SchemaGroup} {
		t.Run(id, func(t *testing.T) {
			res := provisioningRequest(r, "GET", "/scim/v2/Schemas/"+url.PathEscape(id), "")
			if res.Code != 200 {
				t.Errorf("advertised schema %s: %d %s; want 200", id, res.Code, res.Body)
			}
		})
	}
}
