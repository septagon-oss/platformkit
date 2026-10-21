package rest_test

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
)

type storedRevisionArgument struct {
	Revision int64 `json:"revision"`
}

func TestCommandRefusesServerOwnedArgumentsBeforeRegistration(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts rest.CommandOptions
	}{
		{"row", rest.CommandOptions{}},
		{"collection", rest.CommandOptions{Collection: true}},
		{"system", rest.CommandOptions{Present: entity.CommandHints{System: true}}},
		{"operator", rest.CommandOptions{Auth: httpx.OperatorPermission("task:write")}},
		{"public", rest.CommandOptions{Auth: httpx.Public()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			refuseCommandArgument[storedRevisionArgument](t, tc.opts, "revision")
		})
	}
	t.Run("JSON name", func(t *testing.T) {
		refuseCommandArgument[struct {
			Observed int64 `json:"revision,omitempty"`
		}](t, rest.CommandOptions{}, "revision")
	})
	t.Run("pointer", func(t *testing.T) {
		refuseCommandArgument[*storedRevisionArgument](t, rest.CommandOptions{}, "revision")
	})
	t.Run("hidden", func(t *testing.T) {
		refuseCommandArgument[struct {
			Revision int64 `json:"revision" ui:"visibility:hidden"`
		}](t, rest.CommandOptions{}, "revision")
	})
	t.Run("embedded Base before revision", func(t *testing.T) {
		refuseCommandArgument[struct {
			crud.Base
			Revision int64  `json:"revision"`
			Reason   string `json:"reason"`
		}](t, rest.CommandOptions{}, "id")
	})
}

func refuseCommandArgument[I any](t *testing.T, opts rest.CommandOptions, field string) {
	t.Helper()
	api, _ := patchDocumentAPI()
	surfaces := api.Surfaces(spec.Module)
	spec.Mount(surfaces)
	before := api.Resources()
	operations, routes := len(api.Recorded()), len(api.Mounted())
	calls := 0
	var got any
	func() {
		defer func() { got = recover() }()
		rest.Command(surfaces, spec, "confirm", "Confirm", "Confirm the task.", nil,
			func(context.Context, db.Tx[db.Tenant], uuid.UUID, I) (*Task, error) {
				calls++
				return nil, nil
			}, opts)
	}()
	want := fmt.Sprintf("rest: command confirm on tasks.task: argument field %q is server-owned; use a caller-owned argument name instead (ifRevision for an observed revision)", field)
	if got != want {
		t.Errorf("panic = %v, want %q", got, want)
	}
	after := api.Resources()
	if len(after) != len(before) || len(after[0].Commands) != len(before[0].Commands) {
		t.Errorf("refused declaration changed commands: before %v, after %v", before, after)
	}
	if len(api.Recorded()) != operations || len(api.Mounted()) != routes || calls != 0 {
		t.Errorf("refused declaration had effects: operations %d -> %d, routes %d -> %d, calls %d",
			operations, len(api.Recorded()), routes, len(api.Mounted()), calls)
	}
}

func TestCommandAdmitsCallerOwnedArguments(t *testing.T) {
	t.Run("revision condition", func(t *testing.T) {
		admitCommandArgument[struct {
			IfRevision int64 `json:"ifRevision"`
		}](t, "ifRevision")
	})
	t.Run("Go name is irrelevant", func(t *testing.T) {
		admitCommandArgument[struct {
			Revision int64 `json:"ifRevision"`
		}](t, "ifRevision")
	})
	t.Run("noninteger revision", func(t *testing.T) {
		admitCommandArgument[struct {
			Revision string `json:"revision"`
		}](t, "revision")
	})
	t.Run("omitted revision", func(t *testing.T) {
		admitCommandArgument[struct {
			Revision int64 `json:"-"`
		}](t)
	})
	t.Run("no arguments", func(t *testing.T) {
		admitCommandArgument[struct{}](t)
	})
	t.Run("ordinary names and readOnly tag", func(t *testing.T) {
		admitCommandArgument[struct {
			ID        string `json:"id"`
			CreatedAt string `json:"createdAt"`
			Note      string `json:"note" readOnly:"true"`
		}](t, "id", "createdAt", "note")
	})
}

func admitCommandArgument[I any](t *testing.T, names ...string) {
	t.Helper()
	api, _ := patchDocumentAPI()
	surfaces := api.Surfaces(spec.Module)
	spec.Mount(surfaces)
	rest.Command(surfaces, spec, "confirm", "Confirm", "Confirm the task.", nil,
		func(context.Context, db.Tx[db.Tenant], uuid.UUID, I) (*Task, error) { return nil, nil }, rest.CommandOptions{})
	commands := api.Resources()[0].Commands
	if len(commands) != 1 {
		t.Fatalf("commands = %v, want confirm", commands)
	}
	var got []string
	for _, f := range commands[0].Fields {
		got = append(got, f.Name)
	}
	if !slices.Equal(got, names) {
		t.Errorf("fields = %v, want %v", got, names)
	}
	op := recorded(t, api, "tasks-task-confirm")
	if op.Method != http.MethodPost || op.Path != "/api/v1/tasks/task/{id}/confirm" {
		t.Errorf("operation = %s %s", op.Method, op.Path)
	}
}

func TestCommandJSONReceivesCallerOwnedRevisionCondition(t *testing.T) {
	type condition struct {
		IfRevision int64 `json:"ifRevision"`
	}
	api, router, _ := mounted(t)
	id, calls := uuid.New(), 0
	rest.Command(api.Surfaces(spec.Module), spec, "confirm", "Confirm", "Confirm the task.", nil,
		func(_ context.Context, tx db.Tx[db.Tenant], gotID uuid.UUID, in condition) (*Task, error) {
			calls++
			if gotID != id || tx.DB() == nil || db.TenantOf(tx).ID != acme.ID {
				t.Errorf("command lost request identity or tenant transaction: id %s, tenant %v", gotID, db.TenantOf(tx))
			}
			if in.IfRevision != 7 {
				return nil, crud.ErrInvalid
			}
			return &Task{Title: "confirmed"}, nil
		}, rest.CommandOptions{})
	path := "/api/v1/tasks/task/" + id.String() + "/confirm"
	if code, body := call(t, router, http.MethodPost, path, `{"ifRevision":7}`); code != http.StatusOK || !strings.Contains(body, "confirmed") {
		t.Fatalf("confirm = %d %s", code, body)
	}
	if code, body := call(t, router, http.MethodPost, path, `{"ifRevision":0}`); code != http.StatusUnprocessableEntity {
		t.Errorf("invalid condition = %d %s, want 422", code, body)
	}
	if calls != 2 {
		t.Errorf("callback calls = %d, want 2", calls)
	}
}
