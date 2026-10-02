package main

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"

	"github.com/septagon-oss/platformkit/pkit"
)

// journey is one thing a person comes to this application to do, driven over the wire as
// that person. prepare runs as the administrator first, so each journey starts from its
// own task and no row depends on another persona's run.
type journey struct {
	name string
	run  func(t *testing.T, as *http.Client, me string) (int, string)
}

// TestEveryPersonaCanDoItsJourneysAndIsRefusedTheOthers is decision 0011 item 6 at the
// shipped composition: each persona the application seeds signs in and attempts every
// journey, and the table says which it may do. A refusal must be a refusal — 403 with
// the reason's code, or the 404 of a control plane this session cannot see — and never a
// 200 that did nothing or a 500.
//
// Two rows are not about grants. Resolving somebody else's task is refused to the
// administrator and the coordinator though both hold task:update: that is
// policy/task.rego, so the table shows the object scope and the grant as separate
// questions. And the plain member holds nothing, because a shared sign-in grants no
// application access.
func TestEveryPersonaCanDoItsJourneysAndIsRefusedTheOthers(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	raise := func(t *testing.T) string {
		t.Helper()
		code, body := do(t, cfg, admin, http.MethodPost, acmeHost, tasksPath, `{"title":"persona journey","priority":"normal"}`)
		if code != http.StatusCreated {
			t.Fatalf("the administrator raising a task = %d %s", code, body)
		}
		return tasksPath + "/" + field(t, body, "id")
	}
	assign := func(t *testing.T, task, who string) {
		t.Helper()
		if code, body := do(t, cfg, admin, http.MethodPost, acmeHost, task+"/assign", `{"assigneeId":"`+who+`"}`); code != http.StatusOK {
			t.Fatalf("the administrator assigning %s = %d %s", task, code, body)
		}
	}

	journeys := []journey{
		{"follow the desk", func(t *testing.T, as *http.Client, _ string) (int, string) {
			return do(t, cfg, as, http.MethodGet, acmeHost, tasksPath, "")
		}},
		{"raise a task", func(t *testing.T, as *http.Client, _ string) (int, string) {
			return do(t, cfg, as, http.MethodPost, acmeHost, tasksPath, `{"title":"raised by a persona","priority":"low"}`)
		}},
		{"assign a task", func(t *testing.T, as *http.Client, _ string) (int, string) {
			return do(t, cfg, as, http.MethodPost, acmeHost, raise(t)+"/assign", `{"assigneeId":"`+uuid.NewString()+`"}`)
		}},
		{"resolve my task", func(t *testing.T, as *http.Client, me string) (int, string) {
			task := raise(t)
			assign(t, task, me)
			return do(t, cfg, as, http.MethodPost, acmeHost, task+"/resolve", `{"resolution":"done"}`)
		}},
		{"resolve somebody else's task", func(t *testing.T, as *http.Client, _ string) (int, string) {
			task := raise(t)
			assign(t, task, uuid.NewString())
			return do(t, cfg, as, http.MethodPost, acmeHost, task+"/resolve", `{"resolution":"done"}`)
		}},
		{"administer roles", func(t *testing.T, as *http.Client, _ string) (int, string) {
			return do(t, cfg, as, http.MethodGet, acmeHost, "/api/v1/auth/roles", "")
		}},
		{"operate tenants", func(t *testing.T, as *http.Client, _ string) (int, string) {
			return do(t, cfg, as, http.MethodGet, acmeHost, tenantPath, "")
		}},
	}

	// The table. "" is allowed; anything else is the refusal expected, by its code where
	// the answer carries one.
	const allowed = ""
	table := map[string][]string{
		authcontracts.RoleAdmin:  {allowed, allowed, allowed, allowed, "POLICY_DENIED", allowed, allowed},
		"coordinator":            {allowed, allowed, allowed, allowed, "POLICY_DENIED", "AUTH_DENIED", "refused"},
		"observer":               {allowed, "AUTH_DENIED", "AUTH_DENIED", "AUTH_DENIED", "AUTH_DENIED", "AUTH_DENIED", "refused"},
		authcontracts.RoleMember: {"AUTH_DENIED", "AUTH_DENIED", "AUTH_DENIED", "AUTH_DENIED", "AUTH_DENIED", "AUTH_DENIED", "refused"},
	}

	acme := acmeTenant(t, cfg)
	for _, p := range personas {
		if _, ok := table[p.Name]; !ok {
			t.Errorf("persona %q is seeded and has no row in this table; every persona's journeys are proven", p.Name)
		}
	}
	for role, want := range table {
		t.Run(role, func(t *testing.T) {
			as := admin
			if role != authcontracts.RoleAdmin {
				email := role + ".persona@acme.localhost"
				provisionAs(t, cfg, c, acme, email, role)
				as = signIn(t, cfg, acmeHost, email, adminPass)
			}
			me := field(t, whoami(t, cfg, as), "userId")
			for i, j := range journeys {
				code, body := j.run(t, as, me)
				switch expected := want[i]; {
				case expected == allowed && (code < 200 || code > 299):
					t.Errorf("%s: %s = %d %s, want it allowed", role, j.name, code, body)
				case expected == "refused" && code != http.StatusForbidden && code != http.StatusNotFound:
					t.Errorf("%s: %s = %d %s, want it refused", role, j.name, code, body)
				case expected != allowed && expected != "refused" && (code != http.StatusForbidden || !strings.Contains(body, expected)):
					t.Errorf("%s: %s = %d %s, want 403 %s", role, j.name, code, body, expected)
				}
			}
		})
	}
}

// acmeTenant is the tenant the bootstrap created.
func acmeTenant(t *testing.T, cfg config.Config) uuid.UUID {
	t.Helper()
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()
	var raw string
	err = dbtest.System(t.Context(), conn, func(_ context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Raw(`SELECT id::text FROM tenants WHERE slug = 'acme'`).Row().Scan(&raw)
	})
	id, _ := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		t.Fatalf("read acme's id: %v (%s)", err, id)
	}
	return id
}

// provisionAs creates somebody in a tenant holding one role, with the password the
// tests sign in with.
func provisionAs(t *testing.T, cfg config.Config, c composition, tenantID uuid.UUID, email, role string) {
	t.Helper()
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()
	err = dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		_, err := c.users.Provision(ctx, tx, tenantID, email, "", adminPass, []string{role})
		return err
	})
	if err != nil {
		t.Fatalf("provision %s as %s: %v", email, role, err)
	}
}

// TestAPersonaGrantingWhatNoModuleDeclaresIsRefusedAtCompose: a persona naming a
// permission nothing composed declares, or the control plane's own, stops the
// application before bootstrap could seed a role that grants nothing or hands
// every tenant the operator's surface.
//
// It stopped the application by panicking inside compose. Now the application is
// composed in pkit's sentences (app.go), and the same two questions are answered
// there as a refusal with words: pkit.App.Roles records what a tenant begins as,
// and Plan reads the grants off the built manifests. Refused rather than crashed
// is the stronger of the two, so the assertion is the same and the message is a
// sentence a person can act on.
func TestAPersonaGrantingWhatNoModuleDeclaresIsRefusedAtCompose(t *testing.T) {
	_, cfg := configure(t)
	plan := func() error {
		_, err := sentences(cfg, compose(cfg)).Plan(pkit.Deployment{
			Environment: pkit.Development,
			Config:      cfg,
			Transports:  transports(),
		})
		return err
	}
	saved := personas
	t.Cleanup(func() { personas = saved })
	for _, grant := range []string{"invoice:approve", "tenant:manage"} {
		personas = []authcontracts.Role{{Name: "clerk", Grants: authcontracts.Permissions{grant}}}
		err := plan()
		switch {
		case err == nil:
			t.Errorf("a persona granting %q composed", grant)
		case !strings.Contains(err.Error(), grant):
			t.Errorf("a persona granting %q was refused without naming it: %v", grant, err)
		case !strings.Contains(err.Error(), "clerk"):
			t.Errorf("a persona granting %q was refused without naming the role: %v", grant, err)
		}
	}
	personas = saved
	if err := plan(); err != nil {
		t.Errorf("the shipped personas are refused: %v", err)
	}
}
