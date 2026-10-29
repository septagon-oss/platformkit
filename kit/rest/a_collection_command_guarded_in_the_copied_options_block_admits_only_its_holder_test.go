package rest_test

// An author migrating off rest.Command mounts a CommandSpec and copies the
// CommandOptions block they already wrote: the guard stays inside Options and the
// collection route ends up named in both fields that can name it. That is one
// statement of each fact, so it mounts — and what this asks of the command it
// produces is which router serves it and who is admitted there, because an operator
// guard left in the deprecated field still decides the router. No case beside this
// files the pair: the guard spellings are mounted at a row's address and the copied
// collection pair carries no guard at all.

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestACollectionCommandGuardedInTheCopiedOptionsBlockAdmitsOnlyItsHolder(t *testing.T) {
	// An operator grant on a collection route: the operator's router, and no id in
	// the path, which is the address neither fact alone could build.
	const at = "/api/v1/ops/tasks/task/publish"
	declare := func(surfaces httpx.Surfaces) {
		rest.Mount(surfaces, spec, rest.CommandSpec[publishBody]{
			Verb: "publish", Summary: "Publish a note", Doc: "Takes the oldest open task.",
			Collection: true,
			Options: rest.CommandOptions{
				Auth:       httpx.OperatorPermission("task:write"),
				Collection: true,
			},
		}, claimsAnOpenTask)
	}
	installation := tenancy.Tenant{ID: uuid.New(), Slug: "installation", Name: "Installation", Operator: true}
	// The address the mount recorded is asked first, by the caller who holds the
	// guard: the document, the catalogue and the router have to agree before either
	// answer means anything.
	row := uuid.New()
	api, router, admin := mountAs(t, spec, holds{tenant: installation, operator: true},
		func(api *httpx.API) { declare(api.Surfaces(spec.Module)) })
	if _, err := admin.ExecContext(t.Context(),
		"INSERT INTO rest_tasks (id, tenant_id, title) VALUES ($1, $2, $3)", row, installation.ID, "oldest"); err != nil {
		t.Fatal(err)
	}
	op := recorded(t, api, "tasks-task-publish")
	if op == nil {
		t.Fatal("the command mounted no route")
	}
	commands := api.Resources()[0].Commands
	if len(commands) != 1 {
		t.Fatalf("the resource records %d commands, want the one", len(commands))
	}
	if op.Path != at || commands[0].Endpoint != at || !commands[0].Collection {
		t.Errorf("the document mounts at %s and the catalogue publishes %s with Collection %t, want the collection's command at %s",
			op.Path, commands[0].Endpoint, commands[0].Collection, at)
	}
	if code, body := call(t, router, http.MethodPost, at, `{"note":"claimed by the installation"}`); code != http.StatusOK {
		t.Fatalf("POST %s as the holder of the guard = %d %s, want %d", at, code, body, http.StatusOK)
	}
	if got := storedNote(t, admin, row); got != "claimed by the installation" {
		t.Errorf("the row carries %q after its holder ran the command, want the note it was given", got)
	}
	// The same address and the same kind of row, one caller who differs: this tenant's
	// administrator holds every grant that is not the installation's, the Spec's own
	// write permission among them, and may not run the installation's command.
	customer := uuid.New()
	_, tenantRouter, tenantAdmin := mountAs(t, spec, holds{tenant: acme},
		func(api *httpx.API) { declare(api.Surfaces(spec.Module)) })
	if _, err := tenantAdmin.ExecContext(t.Context(),
		"INSERT INTO rest_tasks (id, tenant_id, title) VALUES ($1, $2, $3)", customer, acme.ID, "oldest"); err != nil {
		t.Fatal(err)
	}
	if code, body := call(t, tenantRouter, http.MethodPost, at, `{"note":"claimed by the customer"}`); code == http.StatusOK {
		t.Errorf("POST %s as a caller holding only the tenant's %s = %s, want a refusal: the command is guarded as the installation's work",
			at, spec.Write, body)
	}
	if got := storedNote(t, tenantAdmin, customer); got != "" {
		t.Errorf("the refused command left %q on the row, want nothing written", got)
	}
}
