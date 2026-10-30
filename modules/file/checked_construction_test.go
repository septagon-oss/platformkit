package file_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/contracts/filetest"
	"github.com/septagon-oss/platformkit/pkit"
)

// TestNewCheckedNamesTheRefusalAndNewPanicsWithTheSameSentence: bytes need a store, and a promise that
// a class of them expires needs the tenant list the sweep walks.
func TestNewCheckedNamesTheRefusalAndNewPanicsWithTheSameSentence(t *testing.T) {
	rows := []struct {
		name string
		deps file.Deps
		want string
	}{
		{"no store", file.Deps{},
			"file.Module: Deps.Storage is required"},
		{"a retention class with nobody to walk", file.Deps{Storage: filetest.NewMemory(), Retention: map[string]time.Duration{"receipts": time.Hour}}, "file.Module: Deps.Tenants is required when Deps.Retention names a class"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			_, _, err := file.NewChecked(r.deps)
			if err == nil {
				t.Fatal("NewChecked built a ledger it could not keep")
			}
			if !strings.Contains(err.Error(), r.want) {
				t.Errorf("refusal = %q, want it to contain %q", err, r.want)
			}
			var got any
			func() {
				defer func() { got = recover() }()
				file.New(r.deps)
			}()
			if s, ok := got.(string); !ok || s != err.Error() {
				t.Errorf("panicked with %#v, want the string the checked form answered %q", got, err)
			}
		})
	}
}

// TestNewCheckedBuildsFromItsRequiredDependenciesAlone: the store is the only port
// the ledger needs; quota, ceilings and schedules keep their defaults.
func TestNewCheckedBuildsFromItsRequiredDependenciesAlone(t *testing.T) {
	svc, m, err := file.NewChecked(file.Deps{Storage: filetest.NewMemory()})
	if err != nil {
		t.Fatalf("NewChecked with a store: %v", err)
	}
	if svc == nil {
		t.Fatal("the file service is nil")
	}
	if m.Name != "file" {
		t.Errorf("manifest name = %q, want file", m.Name)
	}
}

// TestACompositionThatRefusesItsOwnStorageRefusesByItsOwnName is the deployment's
// view: a build that refuses says so once, in its own sentence, with the modules
// waiting on it named as not run rather than as failures of their own.
func TestACompositionThatRefusesItsOwnStorageRefusesByItsOwnName(t *testing.T) {
	store := pkit.NewModule("store", func(w *pkit.Wiring) (module.Module, error) {
		svc, manifest, err := file.NewChecked(file.Deps{})
		if err != nil {
			return module.Module{}, err
		}
		pkit.Put[contracts.Service](w, svc)
		return manifest, nil
	}, pkit.Provides[contracts.Service]())
	taker := pkit.NewModule("reader", func(w *pkit.Wiring) (module.Module, error) {
		return module.Module{Name: "reader"}, nil
	}, pkit.Needs[contracts.Service]())

	err := pkit.NewApp("library").Use(store, taker).Validate(pkit.Deployment{Environment: pkit.Development})
	if err == nil {
		t.Fatal("a composition whose file module has no store resolved")
	}
	var refused *pkit.CompositionError
	if !errors.As(err, &refused) {
		t.Fatalf("the refusal is not a composition error: %v", err)
	}
	if len(refused.Problems) == 0 {
		t.Fatal("the composition answered no problem")
	}
	// The refusing module's own sentence leads, prefixed by the module the app
	// named it with. The problem beside it is the same module's undelivered
	// Provides — pkit's own account of a build that returned early — and the
	// module that waited on it is answered in Suppressed, which adds no line: one
	// mistake is one sentence a deployment has to act on.
	if first := refused.Problems[0]; !strings.Contains(first.Sentence, "file.Module: Deps.Storage is required") || first.Module != "store" {
		t.Errorf("the first problem is not the module's refusal: %+v", first)
	}
	for _, p := range refused.Problems[1:] {
		if p.Module != "store" {
			t.Errorf("a module whose build never ran is blamed for its own refusal: %+v", p)
		}
	}
	if len(refused.Suppressed) != 1 || refused.Suppressed[0].Module != "reader" || refused.Suppressed[0].By != "store" {
		t.Errorf("suppressed = %+v, want reader, refused by the store that never built", refused.Suppressed)
	}
}
