package rest_test

// A command's guard answers two questions: which router serves it, and who is
// admitted there. CommandOptions.Auth is one spelling of that guard and
// CommandSpec.Auth the other, so the field an author writes it in may change neither
// answer. Which router each spelling picks is pinned beside this file; what is pinned
// here is the admission, which a mount could get wrong while recording the guard
// correctly — enforcement that fell back to the Spec's own write permission would let
// a customer's administrator, who holds that permission, run a command the
// declaration reserves to the installation. The authorizer below therefore holds the
// operator's grants or a customer's, never both, so the two callers can differ only
// about the guard the command stated.

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// holds is a caller of one tenant holding exactly one half of the grant table: the
// operator's grants, or the grants that are not. It resolves every host to its own
// tenant, so a case reads the authorizer's answer rather than the surface gate's
// concealment, which kit/httpx's TestTheControlPlaneIsNotFoundAtATenantHost tests.
type holds struct {
	tenant   tenancy.Tenant
	operator bool
}

func (h holds) ByHost(context.Context, db.Tx[db.System], string) (tenancy.Tenant, error) {
	return h.tenant, nil
}

func (h holds) Allowed(_ context.Context, _ tenancy.Tenant, g tenancy.Grant) (bool, error) {
	return g.Operator == h.operator, nil
}

// storedNote is what the row carries, read through the owner connection so the
// answer is about the write and not about who is asking.
func storedNote(t *testing.T, admin *sql.DB, row uuid.UUID) string {
	t.Helper()
	var notes string
	if err := admin.QueryRowContext(t.Context(), "SELECT notes FROM rest_tasks WHERE id = $1", row).Scan(&notes); err != nil {
		t.Fatalf("read the row back: %v", err)
	}
	return notes
}

func TestACommandGuardIsEnforcedWhicheverFieldStatesIt(t *testing.T) {
	installation := tenancy.Tenant{ID: uuid.New(), Slug: "installation", Name: "Installation", Operator: true}
	guard := httpx.OperatorPermission("task:write")
	for _, spelled := range []struct {
		name  string
		guard func(*rest.CommandSpec[publishBody])
	}{
		{"at the top level", func(c *rest.CommandSpec[publishBody]) { c.Auth = guard }},
		{"inside the options the positional form used", func(c *rest.CommandSpec[publishBody]) {
			c.Options.Auth = guard
		}},
	} {
		declare := func(surfaces httpx.Surfaces) {
			cmd := rest.CommandSpec[publishBody]{
				Verb: "publish", Summary: "Publish a note", Doc: "Makes the note visible to the tenant.",
			}
			spelled.guard(&cmd)
			rest.Mount(surfaces, spec, cmd, publishesItsOwnRow)
		}
		// The reachability probe first: the address below is one this mount really
		// serves and one somebody can run the command through.
		t.Run(spelled.name+", performed by the caller who holds it", func(t *testing.T) {
			row := uuid.New()
			api, router, admin := mountAs(t, spec, holds{tenant: installation, operator: true},
				func(api *httpx.API) { declare(api.Surfaces(spec.Module)) })
			if _, err := admin.ExecContext(t.Context(),
				"INSERT INTO rest_tasks (id, tenant_id, title) VALUES ($1, $2, $3)", row, installation.ID, "a draft"); err != nil {
				t.Fatal(err)
			}
			op := recorded(t, api, "tasks-task-publish")
			if op == nil {
				t.Fatal("the command mounted no route")
			}
			address := strings.ReplaceAll(op.Path, "{id}", row.String())
			code, body := call(t, router, http.MethodPost, address, `{"note":"published by the installation"}`)
			if code != http.StatusOK {
				t.Fatalf("POST %s as the holder of the guard = %d %s, want %d", address, code, body, http.StatusOK)
			}
			if got := storedNote(t, admin, row); got != "published by the installation" {
				t.Errorf("the row carries %q after its holder ran the command, want the note it was given", got)
			}
		})
		// The same address, the same row, one caller who differs: this tenant's
		// administrator holds task:write, which is the Spec's own write guard, and
		// holds none of the installation's grants.
		t.Run(spelled.name+", refused to a caller holding only the Spec's write", func(t *testing.T) {
			row := uuid.New()
			api, router, admin := mountAs(t, spec, holds{tenant: acme},
				func(api *httpx.API) { declare(api.Surfaces(spec.Module)) })
			if _, err := admin.ExecContext(t.Context(),
				"INSERT INTO rest_tasks (id, tenant_id, title) VALUES ($1, $2, $3)", row, acme.ID, "a draft"); err != nil {
				t.Fatal(err)
			}
			op := recorded(t, api, "tasks-task-publish")
			if op == nil {
				t.Fatal("the command mounted no route")
			}
			address := strings.ReplaceAll(op.Path, "{id}", row.String())
			code, body := call(t, router, http.MethodPost, address, `{"note":"published by the customer"}`)
			if code == http.StatusOK {
				t.Errorf("POST %s as a caller holding only the tenant's %s = %s, want a refusal: the command is guarded as the installation's work",
					address, spec.Write, body)
			}
			if got := storedNote(t, admin, row); got != "" {
				t.Errorf("the refused command left %q on the row, want nothing written", got)
			}
		})
	}
}
