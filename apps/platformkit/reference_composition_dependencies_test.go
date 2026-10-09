package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/pkit"
)

// The public site consumes the site settings service. The composition document
// must show that edge to a reader who is checking which module supplies it.
func TestReferenceCompositionNamesWebsSiteDependency(t *testing.T) {
	cfg := referenceDependencyConfig(t)
	got := composeReference(cfg, pkit.Development).explain(app.All)
	for _, module := range []string{"site.Module", "web.Module"} {
		if !strings.Contains(got, module) {
			t.Fatalf("composition did not include %s, so its dependency was not tested:\n%s", module, got)
		}
	}
	var webLine string
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "web.Module") && strings.Contains(line, "site.Module") && strings.Contains(line, "sitecontracts.Service") {
			return
		}
		if strings.HasPrefix(line, "pkit: web.Module needs ") {
			webLine = line
		}
	}
	if webLine == "" {
		t.Error("the composition says nothing about what web.Module needs")
	} else {
		t.Errorf("the public site needs sitecontracts.Service from site.Module; the composition says %q", webLine)
	}
}

// A module that still has a prebuilt service must not conceal that its provider
// was removed from the app's declared composition.
func TestReferenceCompositionRefusesWebWithoutSiteModule(t *testing.T) {
	cfg := referenceDependencyConfig(t)
	c := compose(cfg)
	if !namesAModule(c.modules, "site") || !namesAModule(c.modules, "web") {
		t.Fatal("the composition must contain site and web before this dependency is tested")
	}
	_, err := sentencesOf(cfg, "site").Plan(pkit.Deployment{Environment: pkit.Development, Config: cfg})
	if err == nil || !strings.Contains(err.Error(), "sitecontracts.Service") || !strings.Contains(err.Error(), "web") {
		t.Errorf("web needs sitecontracts.Service from the removed site module; Plan returned %v", err)
	}
}

func referenceDependencyConfig(t *testing.T) config.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(compositionConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
