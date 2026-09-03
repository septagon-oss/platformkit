package app

import (
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/migrations"
)

// MigrationSources presents migrations/ and every module's SQL to kit/db as one
// directory.
//
// It is one directory because there is one ledger. golang-migrate records a
// single version number, so running a second source after the first would
// compare that source's files against the first source's version and silently
// apply nothing. Merging the file lists instead keeps the ledger honest and
// makes E2's move of module SQL into migrations/ a file rename with no change
// in behaviour.
//
// The price is that version numbers are global. A collision is refused here,
// naming both owners, rather than becoming a migration that never runs.
//
// It is exported because a repository composing its own modules on top of this
// one has to build the same union to test them: db.Migrate run once per source
// does not work — the second call compares its own files against the first's
// recorded version and fails on the version it cannot find — so a test that
// migrates a mixed composition needs the union this builds. The catalogue
// repository wrote its own copy of it, which is one implementation too many for
// the thing that decides what the schema is.
func MigrationSources(mods []module.Module) (fs.FS, error) {
	u := union{files: map[string]fs.FS{}}
	owner := map[string]string{} // version -> the source that claimed it

	add := func(label string, fsys fs.FS) error {
		entries, err := fs.ReadDir(fsys, ".")
		if err != nil {
			return fmt.Errorf("app: migrations of %s: %w", label, err)
		}
		found := 0
		for _, e := range entries {
			if e.IsDir() || !migrationFile.MatchString(e.Name()) {
				continue
			}
			found++
			name := e.Name()
			v := version(name)
			if first, taken := owner[v]; taken && first != label {
				return fmt.Errorf("app: migrations: %s/%s reuses version %s, already claimed by %s; number every migration once across the whole application", label, name, v, first)
			}
			owner[v] = label
			u.files[name] = fsys
			u.names = append(u.names, name)
		}
		if found == 0 {
			// A module that embedded migrations/*.sql rather than the files
			// themselves would otherwise contribute nothing, silently, and its
			// tables would simply never exist.
			return fmt.Errorf("app: migrations of %s: no <version>_<name>.(up|down).sql file at the root of the file system; wrap it in fs.Sub if the files live in a subdirectory", label)
		}
		return nil
	}

	if err := add("migrations/", migrations.FS); err != nil {
		return nil, err
	}
	for _, m := range mods {
		if m.Migrations == nil {
			continue
		}
		if err := add("module "+m.Name, m.Migrations); err != nil {
			return nil, err
		}
	}
	sort.Strings(u.names)
	return u, nil
}

// migrationFile is what golang-migrate will actually read: a version, a name,
// and a direction. Counting anything else as a contribution would let a module
// that shipped a README and no SQL pass the "contributed nothing" check below.
var migrationFile = regexp.MustCompile(`^\d+_.+\.(up|down)\.sql$`)

// version is the numeric prefix golang-migrate orders by, with the leading
// zeros removed because golang-migrate reads it as a number: 1_x and 000001_x
// are one version there, and have to be one version here too.
func version(name string) string {
	v := name
	if i := strings.IndexByte(name, '_'); i > 0 {
		v = name[:i]
	}
	if trimmed := strings.TrimLeft(v, "0"); trimmed != "" {
		return trimmed
	}
	return "0"
}

// union is a flat directory assembled from several file systems. It implements
// exactly what source/iofs asks of it: read the one directory, open a file by
// name.
type union struct {
	files map[string]fs.FS
	names []string
}

func (u union) Open(name string) (fs.File, error) {
	fsys, ok := u.files[name]
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return fsys.Open(name)
}

func (u union) ReadDir(name string) ([]fs.DirEntry, error) {
	if name != "." {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	out := make([]fs.DirEntry, 0, len(u.names))
	for _, n := range u.names {
		info, err := fs.Stat(u.files[n], n)
		if err != nil {
			return nil, err
		}
		out = append(out, fs.FileInfoToDirEntry(info))
	}
	return out, nil
}
