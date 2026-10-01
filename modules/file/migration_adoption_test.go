package file_test

import (
	"io/fs"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/modules/file"
)

// An adoption re-owns the foundation's ledger row of one version as this
// source's file. That is right only while the foundation itself ships no file of
// that version: where it does — as 000030_tenant_oidc does at the number the
// retention file once held — the row at (platformkit, version) is the
// foundation's own, and adopting it would hand this source a row whose checksum
// is another file's. The ledger keys rows by (owner, version), so a shared
// number is not a collision there; it is one in the pre-split world the upgrade
// fixture rebuilds (apps/platformkit/app_test.go, legacyLayout), which flattens
// every owner's files under the foundation's name. So every adopted version is a
// file here and a number the foundation leaves free, and no file here shares a
// number with the foundation.
func TestTheFileSourceAdoptsOnlyVersionsTheFoundationNoLongerShips(t *testing.T) {
	own := versions(t, file.Migrations.Files)
	foundationDir, err := os.ReadDir("../../migrations")
	if err != nil {
		t.Fatalf("read the foundation's migrations: %v", err)
	}
	foundation := map[int64]string{}
	for _, entry := range foundationDir {
		if v, ok := version(entry.Name()); ok {
			foundation[v] = entry.Name()
		}
	}
	if len(foundation) == 0 {
		t.Fatal("the foundation's migrations directory holds no versioned file; the comparison would be empty")
	}
	if len(file.Migrations.Adopts) == 0 {
		t.Fatal("the file source declares no adoption; 19 was applied under platformkit by a release")
	}
	for _, adoption := range file.Migrations.Adopts {
		for _, v := range adoption.Versions {
			if _, ok := own[v]; !ok {
				t.Errorf("the file source adopts %s's version %d but ships no file of that version", adoption.Owner, v)
			}
			if name, ok := foundation[v]; ok && adoption.Owner == "platformkit" {
				t.Errorf("the file source adopts platformkit's version %d, which the foundation still ships as %s: the adoption would take the foundation's own row", v, name)
			}
		}
	}
	for v, name := range own {
		if other, ok := foundation[v]; ok {
			t.Errorf("the file source's %s shares version %d with the foundation's %s", name, v, other)
		}
	}
}

func versions(t *testing.T, files fs.FS) map[int64]string {
	t.Helper()
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		t.Fatalf("read the file source: %v", err)
	}
	out := map[int64]string{}
	for _, entry := range entries {
		if v, ok := version(entry.Name()); ok {
			out[v] = entry.Name()
		}
	}
	if len(out) == 0 {
		t.Fatal("the file source holds no versioned file")
	}
	return out
}

func version(name string) (int64, bool) {
	if !strings.HasSuffix(name, ".up.sql") {
		return 0, false
	}
	digits, _, _ := strings.Cut(name, "_")
	v, err := strconv.ParseInt(digits, 10, 64)
	return v, err == nil
}
