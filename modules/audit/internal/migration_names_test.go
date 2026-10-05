package internal_test

import (
	"io/fs"
	"regexp"
	"testing"

	"github.com/septagon-oss/platformkit/modules/audit"
)

// sixDigits is the file name every migration in this tree carries: a six-digit
// version, an underscore, a name, `.up.sql`.
var sixDigits = regexp.MustCompile(`^[0-9]{6}_[a-z0-9_]+\.up\.sql$`)

// TestMigrationFilesCarrySixDigitVersions holds the module's files to the shape every
// other owner's files have, so a listing sorts by version and a reader finds 41 after 37.
func TestMigrationFilesCarrySixDigitVersions(t *testing.T) {
	names, err := fs.Glob(audit.Migrations.Files, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) == 0 {
		t.Fatal("the module's migration source holds no files, which proves nothing")
	}
	for _, n := range names {
		if !sixDigits.MatchString(n) {
			t.Errorf("migration %q is not <six-digit version>_<name>.up.sql", n)
		}
	}
}
