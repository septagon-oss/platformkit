package db

import (
	"cmp"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type migrationID struct {
	owner   string
	version int64
}

type migration struct {
	migrationID
	name     string
	sql      string
	checksum string
}

var migrationOwner = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
var migrationFile = regexp.MustCompile(`^([0-9]+)_.+\.up\.sql$`)

// Read every file before connecting. An invalid later source must not let an
// earlier capability change the schema, and the executed bytes are the bytes
// whose checksum we record.
func readMigrations(sources []MigrationSource) ([]migration, error) {
	owners := map[string]bool{}
	var all []migration
	for _, source := range sources {
		if !migrationOwner.MatchString(source.Owner) || owners[source.Owner] {
			return nil, fmt.Errorf("db: migrate: invalid or repeated owner %q", source.Owner)
		}
		owners[source.Owner] = true
		if source.Files == nil {
			return nil, fmt.Errorf("db: migrate: %s has no migration files", source.Owner)
		}
		entries, err := fs.ReadDir(source.Files, ".")
		if err != nil {
			return nil, fmt.Errorf("db: migrate: %s: %w", source.Owner, err)
		}
		versions := map[int64]bool{}
		var owned []migration
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
				continue
			}
			name := entry.Name()
			parts := migrationFile.FindStringSubmatch(name)
			if parts == nil {
				return nil, fmt.Errorf("db: migrate: %s/%s: expected <version>_<name>.up.sql", source.Owner, name)
			}
			version, err := strconv.ParseInt(parts[1], 10, 64)
			if err != nil || version < 1 || versions[version] {
				return nil, fmt.Errorf("db: migrate: %s/%s: invalid or repeated version", source.Owner, name)
			}
			versions[version] = true
			body, err := fs.ReadFile(source.Files, name)
			if err != nil {
				return nil, fmt.Errorf("db: migrate: %s/%s: %w", source.Owner, name, err)
			}
			if strings.TrimSpace(string(body)) == "" {
				return nil, fmt.Errorf("db: migrate: %s/%s is empty", source.Owner, name)
			}
			owned = append(owned, migration{
				migrationID: migrationID{owner: source.Owner, version: version},
				name:        name, sql: string(body), checksum: fmt.Sprintf("%x", sha256.Sum256(body)),
			})
		}
		if len(owned) == 0 {
			return nil, fmt.Errorf("db: migrate: %s has no migrations at its root; use fs.Sub for an embedded directory", source.Owner)
		}
		slices.SortFunc(owned, func(a, b migration) int { return cmp.Compare(a.version, b.version) })
		all = append(all, owned...)
	}
	return all, nil
}
