package rest_test

// A `default:"…"` that does not parse as the type beside it is a declaration
// that contradicts itself, and the JSON Schema the native catalog and a
// generated client read is the one place that contradiction has to be resolved.
// kit/rest resolves it by refusing to mount — the way it refuses a widget name
// no screen can draw and a presentation nothing renders — because the
// alternative found in review was a projection refusing per request: the process
// keeps serving, and the catalog nobody can fetch is the only thing that says the
// composition was broken. An application that cannot describe its own records
// does not start to serve them.

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
)

// Streak is an entity whose count carries a default no integer can read.
type Streak struct {
	crud.Base
	Count int `json:"count" default:"three"`
}

func (Streak) TableName() string { return "rest_streaks" }

// SealInput is a command argument carrying the same mistake. The entity behind
// the command is the sound one, which is the point: the Spec's own fields are
// refused at its mount, and nothing there has seen I.
type SealInput struct {
	Sealed int `json:"sealed" default:"soon"`
}

// TestADefaultTheSchemaCannotProjectIsRefusedAtMount mirrors
// TestAWidgetNoScreenCanDrawIsRefusedAtMount, including why the message is the
// assertion: Mount on an API with no router behind it panics for its own
// reasons, so a bare recover() would be satisfied by the wrong accident.
//
// The path, the field and the default all have to be in it. An operator facing a
// module that will not boot has the struct tag to go and fix, and nothing else.
func TestADefaultTheSchemaCannotProjectIsRefusedAtMount(t *testing.T) {
	var recovered any
	defer func() {
		recovered = recover()
		if recovered == nil {
			t.Error("Mount accepted an entity whose default does not parse as its type")
			return
		}
		message, _ := recovered.(string)
		for _, want := range []string{"/api/streaks", `"count"`, `"three"`} {
			if !strings.Contains(message, want) {
				t.Errorf("Mount refused without naming %s: %v", want, recovered)
			}
		}
	}()
	rest.Spec[*Streak]{Module: "streak", Entity: "streak", Path: "/api/streaks",
		Read: "streak:read", Write: "streak:write"}.Mount(&httpx.API{})
}

// TestACommandArgumentTheSchemaCannotProjectIsRefusedAtMount is the other half:
// a command's argument is a schema the catalog carries next to the entity's, and
// it comes from a type the Spec never saw. The verb is named for the same reason
// the path is — one message per door.
func TestACommandArgumentTheSchemaCannotProjectIsRefusedAtMount(t *testing.T) {
	var recovered any
	defer func() {
		recovered = recover()
		if recovered == nil {
			t.Error("a command whose argument cannot be projected mounted quietly")
			return
		}
		message, _ := recovered.(string)
		for _, want := range []string{"seal", `"sealed"`, `"soon"`} {
			if !strings.Contains(message, want) {
				t.Errorf("the command refused without naming %s: %v", want, recovered)
			}
		}
	}()
	api, _, _ := mounted(t)
	rest.Command(api, spec, "seal", "Seal it", "A door with an argument it cannot describe.", nil,
		func(context.Context, db.Tx[db.Tenant], uuid.UUID, SealInput) (*Task, error) { return nil, nil },
		rest.CommandOptions{})
}

// Roster is an entity whose list carries a default the projection refuses and no
// JSON has a reading for. Huma's own tag reader leaves it alone — it refused the
// two integer cases above and not this one — so before the mount refused, a
// Roster booted, served its five routes, and answered everyone who asked
// /api/v1/admin/resources with a 500. That request-time failure is what this gate
// exists for, and the case is kept because it is the one that reproduces it: the
// integer cases pin the wording of the refusal, this one pins the need for it.
type Roster struct {
	crud.Base
	Roles []string `json:"roles" gorm:"-" default:"admin,member"`
}

func (Roster) TableName() string { return "rest_rosters" }

// TestAListDefaultIsRefusedAtMount. The int cases above are refused by huma's tag
// reader as well; this one is refused only here, and it is refused instead of
// booted and paid for by whoever asked what is in this application.
func TestAListDefaultIsRefusedAtMount(t *testing.T) {
	var recovered any
	defer func() {
		recovered = recover()
		if recovered == nil {
			t.Error("a Spec whose list carries an unprojectable default mounted quietly")
			return
		}
		message, _ := recovered.(string)
		for _, want := range []string{"/api/rosters", `"roles"`, `"admin,member"`} {
			if !strings.Contains(message, want) {
				t.Errorf("the mount refused without naming %s: %v", want, recovered)
			}
		}
	}()
	rest.Spec[*Roster]{Module: "roster", Entity: "roster", Path: "/api/rosters",
		Read: "roster:read", Write: "roster:write"}.Mount(&httpx.API{})
}
