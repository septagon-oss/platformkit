package transport_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/events/transport"
	"github.com/septagon-oss/platformkit/kit/request"
)

func TestOperationMetadataRoundTripsAndRefusesPartialFacts(t *testing.T) {
	for _, outcome := range []request.Outcome{"", request.Succeeded, request.Failed, request.PendingApproval} {
		ev := fixed()
		ev.Operation = request.Operation{ID: "Phone.Save_1", Ref: uuid.NewString(), Tenant: ev.TenantID.String(), Person: uuid.NewString(), Revision: 2, Outcome: outcome}
		body, err := json.Marshal(ev)
		if err != nil {
			t.Fatal(err)
		}
		var got transport.Event
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatal(err)
		}
		if got.Operation != ev.Operation {
			t.Errorf("got %+v want %+v", got.Operation, ev.Operation)
		}
		var doc map[string]any
		if err := json.Unmarshal(body, &doc); err != nil {
			t.Fatal(err)
		}
		for _, member := range []string{"operationid", "operationref", "operationtenantid", "operationrevision"} {
			altered := make(map[string]any)
			for key, value := range doc {
				altered[key] = value
			}
			delete(altered, member)
			partial, err := json.Marshal(altered)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(partial, &got); err == nil {
				t.Errorf("missing %s accepted", member)
			}
		}
		for member, value := range map[string]any{"operationid": "bad token", "operationref": uuid.Nil.String(), "operationtenantid": "invalid", "operationpersonid": uuid.Nil.String(), "operationoutcome": "queued", "operationrevision": 0} {
			altered := make(map[string]any)
			for key, val := range doc {
				altered[key] = val
			}
			altered[member] = value
			partial, err := json.Marshal(altered)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(partial, &got); err == nil {
				t.Errorf("invalid %s accepted", member)
			}
		}
	}
	ev := fixed()
	ev.Operation = request.Operation{Outcome: request.Succeeded}
	if _, err := json.Marshal(ev); err == nil {
		t.Error("partial producer metadata accepted")
	}
}
