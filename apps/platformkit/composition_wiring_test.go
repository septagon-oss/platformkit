package main

// wiring.go states what each composed module needs and provides. A module added
// to compose without an entry there would still build — the services are handed
// over by hand in compose — and the committed composition would quietly say it
// needs nobody, which is the sentence decision 0074 rule 4 exists to make
// impossible. This test asks every module the composition builds whether its
// edges were written down and read, and asks each module that says it provides a
// contract whether the resolved plan actually holds the value.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	contentcontracts "github.com/septagon-oss/platformkit/modules/content/contracts"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
	sitecontracts "github.com/septagon-oss/platformkit/modules/site/contracts"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestEveryModuleTheCompositionNamesItsEdges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(compositionConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	r := composeReference(cfg, pkit.Development)
	for _, m := range r.modules {
		if _, ok := r.wires[m.Name]; !ok {
			t.Errorf("%s.Module is composed with no entry in the edge table: say what it needs and what it provides", m.Name)
			continue
		}
		if !r.wires[m.Name].known {
			t.Errorf("%s.Module's edge entry was never read by the resolver", m.Name)
		}
	}

	p := r.plan()
	// Each module that says it provides a contract is asked whether the resolved
	// plan holds the value it put: a declaration the build never filled would be
	// the same silence in the other direction.
	holders := map[string]bool{
		"usercontracts.Service":         holds(p, func() bool { v, ok := pkit.Value[usercontracts.Service](p); return ok && v != nil }),
		"tenantcontracts.Service":       holds(p, func() bool { v, ok := pkit.Value[tenantcontracts.Service](p); return ok && v != nil }),
		"notificationcontracts.Service": holds(p, func() bool { v, ok := pkit.Value[notificationcontracts.Service](p); return ok && v != nil }),
		"authcontracts.Auth":            holds(p, func() bool { v, ok := pkit.Value[authcontracts.Auth](p); return ok && v != nil }),
		"contentcontracts.Service":      holds(p, func() bool { v, ok := pkit.Value[contentcontracts.Service](p); return ok && v != nil }),
		"sitecontracts.Service":         holds(p, func() bool { v, ok := pkit.Value[sitecontracts.Service](p); return ok && v != nil }),
	}
	for contract, filled := range holders {
		if !filled {
			t.Errorf("the module that provides %s put no value the plan can answer with", contract)
		}
	}
}

func holds(_ *pkit.Planned, filled func() bool) bool { return filled() }
