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
	migrationHeader
	name     string
	sql      string
	checksum string
}

// adoption is one ledger row to re-own: the file as the adopting source has
// it, and the owner it was applied under.
type adoption struct {
	migration
	from string
}

var migrationOwner = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
var migrationFile = regexp.MustCompile(`^([0-9]+)_.+\.up\.sql$`)

// Read every file before connecting. An invalid later source must not let an
// earlier capability change the schema, and the executed bytes are the bytes
// whose checksum we record. Adoptions are checked here too: every adopted
// version is a file of the adopting source, and no selected source still lists
// a file it says another source adopted — two owners of one version is the
// ambiguity the ledger exists to refuse.
func readMigrations(sources []MigrationSource) ([]migration, []adoption, error) {
	owners := map[string]bool{}
	var all []migration
	var adoptions []adoption
	for _, source := range sources {
		if !migrationOwner.MatchString(source.Owner) || owners[source.Owner] {
			return nil, nil, fmt.Errorf("db: migrate: invalid or repeated owner %q", source.Owner)
		}
		owners[source.Owner] = true
		if source.Files == nil {
			return nil, nil, fmt.Errorf("db: migrate: %s has no migration files", source.Owner)
		}
		entries, err := fs.ReadDir(source.Files, ".")
		if err != nil {
			return nil, nil, fmt.Errorf("db: migrate: %s: %w", source.Owner, err)
		}
		versions := map[int64]migration{}
		var owned []migration
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
				continue
			}
			name := entry.Name()
			parts := migrationFile.FindStringSubmatch(name)
			if parts == nil {
				return nil, nil, fmt.Errorf("db: migrate: %s/%s: expected <version>_<name>.up.sql", source.Owner, name)
			}
			version, err := strconv.ParseInt(parts[1], 10, 64)
			if _, repeated := versions[version]; err != nil || version < 1 || repeated {
				return nil, nil, fmt.Errorf("db: migrate: %s/%s: invalid or repeated version", source.Owner, name)
			}
			body, err := fs.ReadFile(source.Files, name)
			if err != nil {
				return nil, nil, fmt.Errorf("db: migrate: %s/%s: %w", source.Owner, name, err)
			}
			if strings.TrimSpace(string(body)) == "" {
				return nil, nil, fmt.Errorf("db: migrate: %s/%s is empty", source.Owner, name)
			}
			header, err := parseHeader(string(body))
			if err != nil {
				return nil, nil, fmt.Errorf("db: migrate: %s/%s: %w", source.Owner, name, err)
			}
			m := migration{
				migrationID:     migrationID{owner: source.Owner, version: version},
				migrationHeader: header,
				name:            name, sql: string(body), checksum: fmt.Sprintf("%x", sha256.Sum256(body)),
			}
			// The guard applies from the version the owner says is guarded, which is
			// 0 — every file — for a source with nothing applied yet. A file below
			// the floor cannot carry a marker: that would change applied bytes.
			if version >= source.RulesFrom {
				if err := checkRules(m); err != nil {
					return nil, nil, fmt.Errorf("db: migrate: %s/%s: %w", source.Owner, name, err)
				}
			}
			versions[version] = m
			owned = append(owned, m)
		}
		if len(owned) == 0 {
			return nil, nil, fmt.Errorf("db: migrate: %s has no migrations at its root; use fs.Sub for an embedded directory", source.Owner)
		}
		slices.SortFunc(owned, func(a, b migration) int { return cmp.Compare(a.version, b.version) })
		all = append(all, owned...)
		for _, a := range source.Adopts {
			if !migrationOwner.MatchString(a.Owner) || a.Owner == source.Owner || len(a.Versions) == 0 {
				return nil, nil, fmt.Errorf("db: migrate: %s adopts from an invalid owner %q", source.Owner, a.Owner)
			}
			for _, version := range a.Versions {
				m, ok := versions[version]
				if !ok {
					return nil, nil, fmt.Errorf("db: migrate: %s adopts version %d from %s and has no such file", source.Owner, version, a.Owner)
				}
				adoptions = append(adoptions, adoption{migration: m, from: a.Owner})
			}
		}
	}
	for _, a := range adoptions {
		if slices.ContainsFunc(all, func(m migration) bool { return m.owner == a.from && m.version == a.version }) {
			return nil, nil, fmt.Errorf("db: migrate: %s/%s is adopted from %s, which still lists version %d; one owner per version", a.owner, a.name, a.from, a.version)
		}
	}
	return all, adoptions, nil
}
