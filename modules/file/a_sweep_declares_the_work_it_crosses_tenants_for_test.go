package file_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts/filetest"
)

// declaredCrossTenantAccess is the cross-tenant need this module's manifest
// states, handed to the raw httpx.API these cases build themselves. In an
// application kit/app carries that sentence from the manifest to the kernel, and
// a test that mounts the module's Routes on an API of its own has to state the
// same fact: a token taken without a declaration is refused at composition.
func declaredCrossTenantAccess() map[string]string {
	return map[string]string{"file": "sweep stored bytes no row references, across every tenant"}
}

// TestTheSweepDeclaresTheWorkItCrossesTenantsFor pins the sentence above to the
// manifest it copies: the sweep's reason is the audit record of every
// cross-tenant transaction the module opens, and a test that handed the kernel a
// different one would be testing a composition nobody ships.
func TestTheSweepDeclaresTheWorkItCrossesTenantsFor(t *testing.T) {
	_, m := file.New(file.Deps{Storage: file.Local(t.TempDir()), MaxBytes: filetest.Limit})
	if got, want := m.SystemAccess.Reason, declaredCrossTenantAccess()["file"]; got != want {
		t.Fatalf("the manifest declares cross-tenant work as %q; the mounted routes take a token minted for %q", got, want)
	}
}
