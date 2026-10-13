package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
)

func TestTenantPlacementReadsTheOperatorsMappingFile(t *testing.T) {
	body, err := os.ReadFile(example)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	mapping := filepath.Join(dir, "tenant-apps.txt")
	if err := os.WriteFile(mapping, []byte("# explicit placement\nshop=collect\nschool=academy\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "  tenant_apps: {}") {
		t.Fatal("the configuration fixture has no inline tenant mapping to extend")
	}
	withFile := strings.Replace(string(body), "  tenant_apps: {}",
		"  tenant_apps: {}\n  tenant_apps_file: "+mapping, 1)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(withFile), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("load a valid explicit tenant mapping file: %v", err)
	}
	for tenant, want := range map[string]string{"shop": "collect", "school": "academy"} {
		if got.App.TenantApps[tenant] != want {
			t.Errorf("tenant %s maps to %q, want %q from the operator's file", tenant, got.App.TenantApps[tenant], want)
		}
	}
}
