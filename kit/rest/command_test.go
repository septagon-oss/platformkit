package rest_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestACollectionCommandIsACommandWithoutTheRow.
//
// The route it exists for is redeeming a code: the caller knows the code and
// not the row it belongs to, so POST /api/tasks/{id}/redeem would be asking
// them for the answer. Everything else about it is Command — the Write
// permission, the request's transaction, the fault mapping, and the events
// declaration the boot gate reads back.
func TestACollectionCommandIsACommandWithoutTheRow(t *testing.T) {
	api, router, admin := mounted(t)
	rest.CollectionCommand(api, spec, "claim",
		"Claim the next task", "Takes the oldest open task and marks it done. There may be none.",
		[]string{"tasks.task.claimed"},
		func(ctx context.Context, tx db.Tx[db.Tenant], in claimBody) (*Task, error) {
			if strings.TrimSpace(in.By) == "" {
				return nil, crud.ErrInvalid
			}
			rows, _, err := crud.List[*Task](tx, crud.Query{Limit: 1, Sort: "title"})
			if err != nil {
				return nil, err
			}
			if len(rows) == 0 {
				return nil, crud.ErrNotFound
			}
			task := rows[0]
			task.Done, task.Notes = true, "claimed by "+in.By
			if err := crud.Update(ctx, tx, task, "done", "notes"); err != nil {
				return nil, err
			}
			return task, events.Publish(ctx, tx, "tasks.task.claimed", task)
		})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the command does not declare itself: %v", err)
	}

	// The path is the collection's, with no row in it.
	if code, body := call(t, router, http.MethodPost, "/api/tasks/claim", `{"by":"ada"}`); code != http.StatusNotFound {
		t.Fatalf("claiming from an empty collection = %d %s, want 404", code, body)
	}
	code, body := call(t, router, http.MethodPost, "/api/tasks", `{"title":"first"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST = %d %s, want 201", code, body)
	}

	// The same fault mapping as every other route here: an invalid argument is
	// 422 and not 500, and it is run that refuses it rather than the decoder.
	if code, body := call(t, router, http.MethodPost, "/api/tasks/claim", `{"by":" "}`); code != http.StatusUnprocessableEntity {
		t.Errorf("an empty argument = %d %s, want 422", code, body)
	}

	code, body = call(t, router, http.MethodPost, "/api/tasks/claim", `{"by":"ada"}`)
	if code != http.StatusOK || !strings.Contains(body, "claimed by ada") {
		t.Fatalf("claim = %d %s, want 200 and the row it claimed", code, body)
	}

	// The event the operation declared is the event the handler published, and
	// the declaration is what kit/app checks against the manifests at boot.
	if got := api.Events(); !contains(got, "tasks.task.claimed") {
		t.Errorf("the recorded events are %v; the command declares its own", got)
	}
	var name string
	if err := admin.QueryRowContext(t.Context(),
		`SELECT name FROM platformkit_outbox WHERE name = 'tasks.task.claimed'`).Scan(&name); err != nil {
		t.Errorf("the command's event is not in the outbox: %v", err)
	}
}

// TestACollectionCommandCarriesTheSpecsWritePermission, including the operator
// flag: a screen and a route that disagreed about which kind of permission this
// is would be a customer's wildcard writing the installation's rows.
func TestACollectionCommandCarriesTheSpecsWritePermission(t *testing.T) {
	operator := spec
	operator.OperatorWrite = true
	api, router, _ := mountAs(t, operator, refuses{})
	rest.CollectionCommand(api, operator, "claim", "Claim", "Claim the next task", nil,
		func(context.Context, db.Tx[db.Tenant], claimBody) (*Task, error) {
			t.Error("the handler ran for a caller who holds nothing")
			return nil, errors.New("unreachable")
		})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the command does not declare itself: %v", err)
	}
	if code, body := call(t, router, http.MethodPost, "/api/tasks/claim", `{"by":"ada"}`); code != http.StatusForbidden {
		t.Errorf("claim by a caller who holds nothing = %d %s, want 403", code, body)
	}
	for _, g := range api.Required() {
		if g.Permission == operator.Write && !g.Operator {
			t.Error("the command asks for the bare permission where the Spec says operator")
		}
	}
}

type claimBody struct {
	By string `json:"by" doc:"Who is claiming it"`
}

// refuses is a caller who holds nothing, which is what a fresh member of a
// customer's tenant is against an operator's resource.
type refuses struct{ caller }

func (refuses) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
	return false, nil
}

func contains(all []string, one string) bool {
	for _, s := range all {
		if s == one {
			return true
		}
	}
	return false
}
