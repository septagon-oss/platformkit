package internal_test

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestAProtocolRefusalCanBeDecodedAsASCIMError(t *testing.T) {
	w := compose(t)
	r := provisioningRouter(t, w)
	// First create establishes that the same route, document and caller work.
	body := `{"displayName":"People"}`
	if res := provisioningRequest(r, "POST", "/scim/v2/Groups", body); res.Code != 201 {
		t.Fatalf("create: %d %s", res.Code, res.Body)
	}
	res := provisioningRequest(r, "POST", "/scim/v2/Groups", body)
	if res.Code != 409 {
		t.Fatalf("duplicate must conflict: %d %s", res.Code, res.Body)
	}
	// RFC 7644 section 3.12 requires the Error schema and a string status.
	var document struct {
		Schemas []string `json:"schemas"`
		Status  string   `json:"status"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &document); err != nil {
		t.Errorf("SCIM client cannot decode refusal: %v; body=%s", err, res.Body)
	}
	if !slices.Contains(document.Schemas, "urn:ietf:params:scim:api:messages:2.0:Error") || document.Status != "409" {
		t.Errorf("refusal does not carry SCIM Error schema and string status: %+v", document)
	}
}
