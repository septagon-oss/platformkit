package main

// T-0117's case for the two provider routes at the reference composition, and
// for the step the tenant module's own test could not take.
//
// modules/tenant/internal/oidc_test.go asserts that `tenant.oidc_set` is in the
// outbox with a payload that enumerates the provider and holds a secret
// *reference*. That is half the claim: the outbox is a queue, and what a person
// reads afterwards is `audit_events`, which modules/audit fills by subscribing
// to whatever the relay carries. Only a running application spans the two, so
// only one can say that the decision "these people sign in at that directory"
// is on the record — and that the record is safe to read: the same row an
// operator's colleague can open must not be where a client secret lives.
//
// It is also the first time the composition was asked to *succeed* at either
// route. The six control-plane routes beside them are driven in app_test.go
// (create, read, suspend, hosts, locale, invite) and probed for absence at a
// tenant's host; these two appear in that probe list below but had never been
// answered, which is how a command that could not write a single row reached a
// committed migration.

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

// The provider this case names for acme. `oidcRef` is a name; `notASecretRef` is
// what a mistake at a keyboard looks like — a value with the shape of a secret
// rather than the shape of a name. It is sent once, refused on the way in, and
// the assertions below are that the trail holds the name and never the value.
const (
	oidcIssuer    = "https://idp.acme.example/realms/people"
	oidcClient    = "acme-portal"
	oidcRef       = "PLATFORMKIT_OIDC_ACME_SECRET"
	notASecretRef = "a-client-secret-in-the-body"
)

func TestTheOperatorNamesATenantsProviderAndTheTrailKeepsItsNameNotItsSecret(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	op := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	id := acmeID(t, cfg, op)
	owner := dbtest.Open(t, cfg.Database.MigrateURL)

	// Half a provider is refused where it arrives, with the field an operator has
	// to fix named. That it wrote nothing is asserted below, after the good write
	// has had its turn in the trail: a count of zero taken here would also be the
	// count of an event still sitting in the queue.
	code, body := do(t, cfg, op, http.MethodPost, acmeHost, tenantPath+"/"+id+"/oidc",
		`{"issuer":"`+oidcIssuer+`","clientId":"`+oidcClient+`","secretRef":"`+notASecretRef+`"}`)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("a secret written where its name belongs = %d %s, want 422", code, body)
	}
	if !strings.Contains(body, "secretRef") {
		t.Errorf("the refusal does not name the field an operator has to fix: %d %s", code, body)
	}

	if code, body := do(t, cfg, op, http.MethodPost, acmeHost, tenantPath+"/"+id+"/oidc",
		`{"issuer":"`+oidcIssuer+`","clientId":"`+oidcClient+`","secretRef":"`+oidcRef+`","registration":"existing"}`); code != http.StatusOK {
		t.Fatalf("naming acme's provider = %d %s, want 200", code, body)
	}

	// Exactly one row is how a refusal that wrote nothing is told from one that
	// published anyway: had the refused attempt been recorded, the trail would
	// hold two and this would wait out its deadline. One row is also the proof
	// that the composition carried the event out of the outbox and into the trail
	// — the step no test below a running application can make.
	var payload string
	eventually(t, "exactly one tenant.oidc_set row in the audit trail", func() bool {
		var rows int
		err := owner.QueryRowContext(t.Context(),
			`SELECT count(*), coalesce(max(payload::text), '') FROM audit_events WHERE name = $1`,
			contracts.EventOIDCSet).Scan(&rows, &payload)
		return err == nil && rows == 1
	})
	if !strings.Contains(payload, oidcRef) || !strings.Contains(payload, oidcIssuer) {
		t.Errorf("the trail's row names neither the issuer nor the reference: %s", payload)
	}
	if strings.Contains(payload, notASecretRef) {
		t.Errorf("the value the route refused is in the trail: %s", payload)
	}
	if strings.Contains(payload, "clientSecret") || strings.Contains(payload, "client_secret") {
		t.Errorf("the audit row carries a member that could hold a client secret: %s", payload)
	}

	// Taking it away is a decision of its own, and the trail says which one.
	if code, body := do(t, cfg, op, http.MethodPost, acmeHost, tenantPath+"/"+id+"/oidc/clear", ""); code != http.StatusOK {
		t.Fatalf("clearing acme's provider = %d %s, want 200", code, body)
	}
	eventually(t, "the clearing reaches the audit trail as its own event", func() bool {
		var rows int
		err := owner.QueryRowContext(t.Context(),
			`SELECT count(*) FROM audit_events WHERE name = $1 AND payload->>'issuer' = $2`,
			contracts.EventOIDCCleared, oidcIssuer).Scan(&rows)
		return err == nil && rows == 1
	})
	// And the clearing did not also read as a second setting: a trail that says
	// "set, set, cleared" tells a person the tenant had three providers.
	if got := countAudit(t, owner, contracts.EventOIDCSet); got != 1 {
		t.Errorf("after the clearing the trail holds %d %s rows, want the one set write", got, contracts.EventOIDCSet)
	}
}

// acmeID asks the control plane which tenant the operator is naming, rather
// than carrying a uuid out of the installer: the route takes an id, and the
// only honest source of one in a composed application is its own list.
func acmeID(t *testing.T, cfg config.Config, op *http.Client) string {
	t.Helper()
	code, body := do(t, cfg, op, http.MethodGet, acmeHost, tenantPath, "")
	if code != http.StatusOK {
		t.Fatalf("GET %s = %d %s, want the operator's list", tenantPath, code, body)
	}
	var list struct {
		Items []struct {
			ID    string   `json:"id"`
			Hosts []string `json:"hosts"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &list); err != nil {
		t.Fatalf("read the tenant list: %v\n%s", err, body)
	}
	for _, item := range list.Items {
		for _, host := range item.Hosts {
			if host == acmeHost {
				return item.ID
			}
		}
	}
	t.Fatalf("the operator's list holds no tenant served at %s: %s", acmeHost, body)
	return ""
}

// countAudit counts one event's rows in the trail modules/audit keeps.
func countAudit(t *testing.T, owner *sql.DB, name string) int {
	t.Helper()
	var rows int
	if err := owner.QueryRowContext(t.Context(),
		`SELECT count(*) FROM audit_events WHERE name = $1`, name).Scan(&rows); err != nil {
		t.Fatalf("count %s in the trail: %v", name, err)
	}
	return rows
}
