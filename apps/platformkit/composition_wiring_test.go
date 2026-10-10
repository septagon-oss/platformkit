package main

// Every edge between the modules this application composes is a contract one of
// the two declares, in that module's own source, so there is no edge table here
// to keep in step with the composition — and no way for a module to be composed
// with its dependencies unwritten down except by not resolving at all. What this
// file asks, then, is the two things that replaced the table: that the resolved
// composition places every module the sentence names, and that each module that
// says it provides a contract put a value the plan can answer with.
//
// The second half is the one that can still rot: a declaration a build never
// filled is the same silence in the other direction, and pkit answers it at
// Build, which these cases read rather than assert around.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/richtext"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	contentcontracts "github.com/septagon-oss/platformkit/modules/content/contracts"
	filecontracts "github.com/septagon-oss/platformkit/modules/file/contracts"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
	sitecontracts "github.com/septagon-oss/platformkit/modules/site/contracts"
	taskcontracts "github.com/septagon-oss/platformkit/modules/task/contracts"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

// sentencesOf is the application's sentence with only its app in hand, for a
// case that asks the resolver a question and reads nothing else. The four values
// sentences also returns belong to the composition's own chrome.
func sentencesOf(cfg config.Config, without ...string) *pkit.App {
	a, _, _, _, _ := sentences(cfg, without...)
	return a
}

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
	// Every module the sentence names is one the resolver placed, and every
	// module the resolver placed is one the sentence named: Use is the whole of
	// what is composed, and nothing is found that Use does not name.
	for _, name := range []string{
		"product", "seed", "user", "tenant", "notification", "auth", "emailregistration", "file",
		"task", "billing", "content", "site", "web", "audit", "change", "access", "admin",
	} {
		if !namesAModule(r.modules, name) {
			t.Errorf("%s.Module is named in Use and answers no manifest in the resolved composition", name)
		}
	}
	if len(r.modules) != 17 {
		t.Errorf("the composition resolved to %d modules, want the 17 the sentence names: %v", len(r.modules), moduleNames(r.modules))
	}
	// The shell is last, and for the same kind of reason audit is next to last:
	// it generates a screen for every resource the modules above it mounted.
	if r.modules[len(r.modules)-1].Name != "admin" {
		t.Errorf("the composition builds %s last; the shell must be the module after everything",
			r.modules[len(r.modules)-1].Name)
	}

	p := r.plan()
	// Each module that says it provides a contract is asked whether the resolved
	// plan holds the value it put.
	holders := map[string]bool{
		"usercontracts.Service":                         holds(p, func() bool { v, ok := pkit.Value[usercontracts.Service](p); return ok && v != nil }),
		"tenantcontracts.Service":                       holds(p, func() bool { v, ok := pkit.Value[tenantcontracts.Service](p); return ok && v != nil }),
		"notificationcontracts.Service":                 holds(p, func() bool { v, ok := pkit.Value[notificationcontracts.Service](p); return ok && v != nil }),
		"notificationcontracts.Mailer":                  holds(p, func() bool { v, ok := pkit.Value[notificationcontracts.Mailer](p); return ok && v != nil }),
		"authcontracts.Auth":                            holds(p, func() bool { v, ok := pkit.Value[authcontracts.Auth](p); return ok && v != nil }),
		"contentcontracts.Service":                      holds(p, func() bool { v, ok := pkit.Value[contentcontracts.Service](p); return ok && v != nil }),
		"sitecontracts.Service":                         holds(p, func() bool { v, ok := pkit.Value[sitecontracts.Service](p); return ok && v != nil }),
		"filecontracts.Service":                         holds(p, func() bool { v, ok := pkit.Value[filecontracts.Service](p); return ok && v != nil }),
		"taskcontracts.Service":                         holds(p, func() bool { v, ok := pkit.Value[taskcontracts.Service](p); return ok && v != nil }),
		"richtext.Files (what file hands content)":      holds(p, func() bool { v, ok := pkit.Value[richtext.Files](p); return ok && v != nil }),
		"jobs.TenantLister (what tenant hands a sweep)": holds(p, func() bool { v, ok := pkit.Value[jobs.TenantLister](p); return ok && v != nil }),
	}
	for contract, filled := range holders {
		if !filled {
			t.Errorf("the module that provides %s put no value the plan can answer with", contract)
		}
	}
}

func holds(_ *pkit.Planned, filled func() bool) bool { return filled() }

func namesAModule(mods []module.Module, name string) bool {
	for _, m := range mods {
		if m.Name == name {
			return true
		}
	}
	return false
}

func moduleNames(mods []module.Module) []string {
	out := make([]string, 0, len(mods))
	for _, m := range mods {
		out = append(out, m.Name)
	}
	return out
}
