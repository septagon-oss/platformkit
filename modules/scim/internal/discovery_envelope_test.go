package internal_test

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/modules/scim/contracts"
)

// RFC 7644 section 4 requires ListResponse for both discovery collections.
func TestDiscoveryCollectionsUseTheSCIMListEnvelope(t *testing.T) {
	w := compose(t)
	r := provisioningRouter(t, w)
	for _, path := range []string{"/scim/v2/Schemas", "/scim/v2/ResourceTypes"} {
		t.Run(path, func(t *testing.T) {
			res := provisioningRequest(r, "GET", path, "")
			if res.Code != 200 {
				t.Fatalf("discovery: %d %s", res.Code, res.Body)
			}
			var list struct {
				Schemas      []string          `json:"schemas"`
				TotalResults int               `json:"totalResults"`
				Resources    []json.RawMessage `json:"Resources"`
			}
			if err := json.Unmarshal(res.Body.Bytes(), &list); err != nil {
				t.Fatalf("SCIM client cannot decode discovery as ListResponse: %v", err)
			}
			if !slices.Contains(list.Schemas, contracts.SchemaListResponse) || list.TotalResults != len(list.Resources) || len(list.Resources) == 0 {
				t.Errorf("invalid discovery ListResponse: %s", res.Body)
			}
		})
	}
}
