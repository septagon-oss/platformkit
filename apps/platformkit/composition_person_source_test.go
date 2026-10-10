package main

// The header's person line is only as real as the wiring beneath it: admin reads
// the signed-in person through usercontracts.Service, so the resolved composition
// must hand admin that contract from the user module, built after it, in every
// environment. The committed COMPOSITION files are goldens — a regeneration
// overwrites them — so this asks the resolved plan itself, where a lost edge
// cannot be written over.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestAdminIsComposedWithTheUserServiceThatNamesThePerson(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(compositionConfig), 0o600); err != nil {
		t.Fatalf("write the configuration: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load the configuration: %v", err)
	}
	for _, env := range []pkit.Environment{pkit.Development, pkit.Production} {
		described, err := composeReference(cfg, env).describe()
		if err != nil {
			t.Fatalf("%s: the reference composition does not describe itself: %v", env, err)
		}
		var doc struct {
			Modules []struct {
				Name string `json:"name"`
				Uses []struct {
					Contract string `json:"contract"`
					From     string `json:"from"`
				} `json:"uses"`
				After []string `json:"after"`
			} `json:"modules"`
		}
		if err := json.Unmarshal(described, &doc); err != nil {
			t.Fatalf("%s: parse the described composition: %v", env, err)
		}
		var admin *struct {
			uses  bool
			after bool
		}
		for _, m := range doc.Modules {
			if m.Name != "admin" {
				continue
			}
			admin = &struct {
				uses  bool
				after bool
			}{}
			for _, u := range m.Uses {
				if u.Contract == "usercontracts.Service" && u.From == "user" {
					admin.uses = true
				}
			}
			for _, a := range m.After {
				if a == "user" {
					admin.after = true
				}
			}
		}
		if admin == nil {
			t.Errorf("%s: the composition resolves no admin module", env)
			continue
		}
		if !admin.uses {
			t.Errorf("%s: admin is not composed with usercontracts.Service from user — the header would have no name to say for the signed-in person", env)
		}
		if !admin.after {
			t.Errorf("%s: admin does not build after user, so the service it reads the person through could be wired before it exists", env)
		}
	}
}
