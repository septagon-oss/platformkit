package internal_test

import (
	"encoding/json"
	"testing"

	"github.com/septagon-oss/platformkit/modules/scim/contracts"
)

func TestReplacingAnInvitedUserHonoursTheActiveAttribute(t *testing.T) {
	w := compose(t)
	r := provisioningRouter(t, w)
	res := provisioningRequest(r, "POST", "/scim/v2/Users", `{"userName":"person@acme.test","active":false}`)
	if res.Code != 201 {
		t.Fatalf("create: %d %s", res.Code, res.Body)
	}
	var person contracts.UserResource
	if err := json.Unmarshal(res.Body.Bytes(), &person); err != nil {
		t.Fatal(err)
	}
	res = provisioningRequest(r, "PUT", "/scim/v2/Users/"+person.ID.String(), `{"userName":"person@acme.test","active":true}`)
	if res.Code != 200 {
		t.Fatalf("replace: %d %s", res.Code, res.Body)
	}
	if err := json.Unmarshal(res.Body.Bytes(), &person); err != nil {
		t.Fatal(err)
	}
	if !person.Active {
		t.Errorf("PUT active:true returned success but left the invited person inactive: %s", res.Body)
	}
}
