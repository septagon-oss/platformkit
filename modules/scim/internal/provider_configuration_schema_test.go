package internal_test

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestProviderConfigurationUsesTheSCIMDiscoverySchema(t *testing.T) {
	w := compose(t)
	res := provisioningRequest(provisioningRouter(t, w), "GET", "/scim/v2/ServiceProviderConfig", "")
	if res.Code != 200 {
		t.Fatalf("discovery: %d %s", res.Code, res.Body)
	}
	// RFC 7643 section 5 defines these fields independently of this server's
	// Go representation. A client must be able to decode that protocol shape.
	var document struct {
		Schemas []string `json:"schemas"`
		ETag    *struct {
			Supported *bool `json:"supported"`
		} `json:"etag"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(document.Schemas, "urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig") {
		t.Errorf("configuration does not identify its SCIM schema: %v", document.Schemas)
	}
	if document.ETag == nil || document.ETag.Supported == nil {
		t.Errorf("configuration omits the required etag.supported declaration: %s", res.Body)
	}
}
