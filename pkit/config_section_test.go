package pkit_test

// A module that cannot decide one of its own settings reads one section of the
// deployment's configuration with pkit.Config, and reads the app's own colours,
// front door and copy with Wiring.Skin. Both are reads of state the resolver
// already holds: Deployment.Config arrives with the deployment, and the three
// customisations are what Theme, Home and Languages recorded.
//
// The two refusals here are the two ways a build can abuse them. Naming no
// section is a module that reads *a* setting without saying which — it is that
// module's own defect, so Build answers it. And Skin carries no module's words
// during a build: the label of a permission belongs to the module that defines
// it, and while one module is being built no other manifest is a built thing.

import (
	"reflect"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
)

func auditReader(got *config.Audit) *pkit.Module {
	return pkit.NewModule("audit", func(w *pkit.Wiring) (module.Module, error) {
		*got = pkit.Config(w, func(c config.Config) config.Audit { return c.Audit })
		return module.Module{Name: "audit"}, nil
	})
}

func silent(name string) *pkit.Module {
	return pkit.NewModule(name, func(w *pkit.Wiring) (module.Module, error) {
		return module.Module{Name: name}, nil
	})
}

func withAudit(cfg config.Config) pkit.Deployment {
	return pkit.Deployment{Environment: pkit.Development, Config: cfg}
}

// TestAModuleReadsTheSectionOfItsDeploymentAndSaysSo is the read and its
// sentence: the value arrives from the deployment the composition is resolved
// for, and Explain prints one line naming the module, the section and nothing
// else — no line for the module that read nothing, and no line that repeats a
// section a module asked for twice.
func TestAModuleReadsTheSectionOfItsDeploymentAndSaysSo(t *testing.T) {
	var kept config.Audit
	app := pkit.NewApp("collect").Use(auditReader(&kept), silent("shop"))

	if err := app.Validate(withAudit(config.Config{Audit: config.Audit{RetentionDays: 90}})); err != nil {
		t.Fatalf("a module reading the section it named was refused: %v", err)
	}
	if kept.RetentionDays != 90 {
		t.Fatalf("audit built with RetentionDays %d, want the deployment's 90", kept.RetentionDays)
	}

	out, err := app.Explain(withAudit(config.Config{Audit: config.Audit{RetentionDays: 90}}))
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if got := strings.Count(out, "pkit: audit.Module reads config.Audit.\n"); got != 1 {
		t.Fatalf("Explain printed the read %d times, want once:\n%s", got, out)
	}
	if strings.Contains(out, "shop.Module reads") {
		t.Fatalf("Explain credited shop, which read no section:\n%s", out)
	}

	// A deployment that says nothing about retention answers the zero value,
	// which is what lets the module keep its own default rather than invent one
	// here: 90 became 0 because nobody wrote 90, not because the read failed.
	var zeroed config.Audit
	if err := pkit.NewApp("collect").Use(auditReader(&zeroed)).Validate(withAudit(config.Config{})); err != nil {
		t.Fatalf("a module reading a section the deployment says nothing about was refused: %v", err)
	}
	if zeroed.RetentionDays != 0 {
		t.Fatalf("the unread section arrived as %d, want the zero value", zeroed.RetentionDays)
	}
}

// TestAModuleThatReadsASectionWithoutNamingOneIsRefused: Config with no section
// function cannot say what it read, so it reads nothing, writes nothing into
// Explain, and the composition is refused with the module's own name in the
// sentence. A refused build hands back no value, not a zero to be noticed later.
func TestAModuleThatReadsASectionWithoutNamingOneIsRefused(t *testing.T) {
	kept := config.Audit{RetentionDays: 365}
	app := pkit.NewApp("collect").Use(pkit.NewModule("audit", func(w *pkit.Wiring) (module.Module, error) {
		kept = pkit.Config[config.Audit](w, nil)
		return module.Module{Name: "audit"}, nil
	}))

	err := app.Validate(withAudit(config.Config{Audit: config.Audit{RetentionDays: 90}}))
	if err == nil {
		t.Fatal("a build that read a section it did not name was accepted")
	}
	for _, want := range []string{"audit", "configuration section"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal says nothing about %q: %v", want, err)
		}
	}
	if kept.RetentionDays != 0 {
		t.Fatalf("the refused read left %d in the module, want the zero value", kept.RetentionDays)
	}
	if _, err := app.Explain(withAudit(config.Config{})); err == nil {
		t.Fatal("Explain answered for a composition Validate refuses")
	}
}

// TestABuildReadsTheAppsSkinAndNoModulesWords pins both halves of what a module
// may read about how the application looks: what the app itself recorded is
// there, and what another module's manifest would say is not. The same value,
// once every module is built, does answer a label — that is the half that makes
// the refusal above a fact about build time rather than about Skin.
func TestABuildReadsTheAppsSkinAndNoModulesWords(t *testing.T) {
	var seen pkit.Skin
	labelled := pkit.NewModule("grant", func(w *pkit.Wiring) (module.Module, error) {
		pkit.Put(w, grantValue{})
		return module.Module{Name: "grant", Permissions: []module.Permission{{Key: "grant:write", Label: "write a grant"}}}, nil
	}, pkit.Provides[grantValue]())
	drawer := pkit.NewModule("drawer", func(w *pkit.Wiring) (module.Module, error) {
		seen = w.Skin()
		return module.Module{Name: "drawer"}, nil
	}, pkit.Needs[grantValue]())

	app := pkit.NewApp("collect").Use(labelled, drawer, doors, desk).Theme(design.Default()).Home("/start")
	plan, err := app.Plan(dev)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if seen.Home != "/start" {
		t.Fatalf("the build was handed front door %q, want the one the app named", seen.Home)
	}
	if !reflect.DeepEqual(seen.Theme, design.Default()) {
		t.Fatal("the build was handed no theme the app named with Theme")
	}
	if got := seen.Label("grant:write"); got != "" {
		t.Fatalf("a build read %q off another module's manifest, which is not built yet", got)
	}
	if got := plan.Skin().Label("grant:write"); got != "write a grant" {
		t.Fatalf("the app's Skin answered %q, want the words the defining module chose", got)
	}
}

type grantValue struct{}
