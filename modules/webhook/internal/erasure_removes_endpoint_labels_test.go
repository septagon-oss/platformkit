package internal_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/modules/webhook/contracts"
	"github.com/septagon-oss/platformkit/modules/webhook/internal"
)

func TestErasureRemovesThePersonsNameAndAddressFromEndpointLabels(t *testing.T) {
	to := newReceiver(t, http.StatusOK)
	e := newEnv(t, true, pool(to.server.Certificate()), nil)
	person := personaldata.Subject{
		ID: uuid.New(), Email: "jo.jones@example.com", DisplayName: "Jo Jones",
	}
	var endpoint *contracts.Endpoint
	e.in(acme, person.ID, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		issued, err := e.svc.Create(ctx, tx, &contracts.Endpoint{
			Name: person.DisplayName + " <" + person.Email + ">",
			URL:  to.server.URL, Events: contracts.EventNames{"task.assigned"},
		})
		if err == nil {
			endpoint = issued.Endpoint
		}
		return err
	})
	part := internal.PersonalData()
	e.in(acme, person.ID, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		body, err := part.Data.Export(ctx, tx, person)
		if err != nil {
			return err
		}
		var exported struct {
			Endpoints []struct {
				ID uuid.UUID `json:"id"`
			} `json:"endpoints"`
		}
		if err := json.Unmarshal(body, &exported); err != nil {
			return err
		}
		if len(exported.Endpoints) != 1 || exported.Endpoints[0].ID != endpoint.ID {
			t.Fatalf("export did not reach the registered endpoint: %s", body)
		}
		return part.Data.Erase(ctx, tx, person)
	})

	var name, url, status string
	var creator uuid.UUID
	e.in(acme, person.ID, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT name, url, status, created_by FROM webhook_endpoints WHERE id = ?`,
			endpoint.ID).Row().Scan(&name, &url, &status, &creator)
	})
	for _, identifier := range []string{person.Email, person.DisplayName} {
		if strings.Contains(strings.ToLower(name), strings.ToLower(identifier)) {
			t.Errorf("committed erasure left %q in endpoint label %q", identifier, name)
		}
	}
	if url != endpoint.URL || status != endpoint.Status || creator != person.ID {
		t.Errorf("erasure changed the endpoint's destination, lifecycle or attribution: %q %q %s", url, status, creator)
	}
}
