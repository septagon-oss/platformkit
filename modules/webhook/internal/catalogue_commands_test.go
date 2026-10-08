package internal_test

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts/tenanttest"
	"github.com/septagon-oss/platformkit/modules/webhook"
	"github.com/septagon-oss/platformkit/ui/screens"
)

func TestTheWebhookCatalogueOffersRegistrationRotationAndReplay(t *testing.T) {
	api, _ := httpx.New(httpx.Options{
		Unwired: true, Cache: cache.Memory("webhook-catalogue"),
		Tenants: tenanttest.NewFake(), Authorize: catalogueDenied{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{}, false, nil
		},
	})
	webhook.New(webhook.Deps{SecretKey: make([]byte, 32), Catalogue: catalogue(emitted)}).Routes(api.Surfaces("webhook"))
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	resources := api.Resources()
	for _, entity := range []string{"endpoint", "delivery"} {
		i := slices.IndexFunc(resources, func(r httpx.Resource) bool { return r.Module == "webhook" && r.Entity == entity })
		if i < 0 {
			t.Fatalf("no webhook/%s resource", entity)
		}
		entry := screens.Describe1(resources[i], true)
		var verbs []string
		for _, command := range entry.Commands {
			verbs = append(verbs, command.Verb)
		}
		if entity == "endpoint" {
			if !slices.Contains(entry.Operations, "create") && !slices.ContainsFunc(entry.Commands, func(c screens.Command) bool { return c.Collection }) {
				t.Errorf("endpoint catalogue offers no registration: operations=%v commands=%v", entry.Operations, verbs)
			}
			if !slices.Contains(verbs, "rotate") {
				t.Errorf("endpoint catalogue offers no secret rotation: commands=%v", verbs)
			}
		} else if !slices.Contains(verbs, "replay") {
			t.Errorf("delivery catalogue offers no replay: commands=%v", verbs)
		}
	}
}

// No request is sent: this test asks the declarations before authorization filters them.
type catalogueDenied struct{}

func (catalogueDenied) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
	return false, nil
}
