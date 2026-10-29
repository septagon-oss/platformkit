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
	// rulesFrom and head are the floor the file's source declared and the highest
	// version that source lists, carried on the file because the refusal of a floor
	// past its own head names both, and because the pass that judges such a file runs
	// after the ledger has been read.
	rulesFrom, head int64
}

// adoption is one ledger row to re-own: the file as the adopting source has
// it, and the owner it was applied under.
type adoption struct {
	migration
	from string
}

var migrationOwner = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
var migrationFile = regexp.MustCompile(`^([0-9]+)_.+\.up\.sql$`)

// Read and validate all sources before connecting so a refused later source
// cannot leave earlier migrations applied. Checksums cover the executed bytes.
func readMigrations(sources []MigrationSource) ([]migration, []adoption, error) {
	owners := map[string]bool{}
	var all []migration
	var adoptions []adoption
	for _, source := range sources {
		if !migrationOwner.MatchString(source.Owner) || owners[source.Owner] {
			return nil, nil, fmt.Errorf("db: migrate: invalid or repeated owner %q", source.Owner)
		}
		owners[source.Owner] = true
		owned, versions, err := readSourceMigrations(source)
		if err != nil {
			return nil, nil, err
		}
		if err := checkSourceRules(source, owned); err != nil {
			return nil, nil, err
		}
		all = append(all, owned...)
		adopted, err := readAdoptions(source, versions)
		if err != nil {
			return nil, nil, err
		}
		adoptions = append(adoptions, adopted...)
	}
	for _, a := range adoptions {
		if slices.ContainsFunc(all, func(m migration) bool { return m.owner == a.from && m.version == a.version }) {
			return nil, nil, fmt.Errorf("db: migrate: %s/%s is adopted from %s, which still lists version %d; one owner per version", a.owner, a.name, a.from, a.version)
		}
	}
	return all, adoptions, nil
}

func readSourceMigrations(source MigrationSource) ([]migration, map[int64]migration, error) {
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
		m, err := readMigration(source, entry.Name(), versions)
		if err != nil {
			return nil, nil, err
		}
		versions[m.version] = m
		owned = append(owned, m)
	}
	if len(owned) == 0 {
		return nil, nil, fmt.Errorf("db: migrate: %s has no migrations at its root; use fs.Sub for an embedded directory", source.Owner)
	}
	slices.SortFunc(owned, func(a, b migration) int { return cmp.Compare(a.version, b.version) })
	return owned, versions, nil
}

func readMigration(source MigrationSource, name string, versions map[int64]migration) (migration, error) {
	parts := migrationFile.FindStringSubmatch(name)
	if parts == nil {
		return migration{}, fmt.Errorf("db: migrate: %s/%s: expected <version>_<name>.up.sql", source.Owner, name)
	}
	version, err := strconv.ParseInt(parts[1], 10, 64)
	if _, repeated := versions[version]; err != nil || version < 1 || repeated {
		return migration{}, fmt.Errorf("db: migrate: %s/%s: invalid or repeated version", source.Owner, name)
	}
	body, err := fs.ReadFile(source.Files, name)
	if err != nil {
		return migration{}, fmt.Errorf("db: migrate: %s/%s: %w", source.Owner, name, err)
	}
	if strings.TrimSpace(string(body)) == "" {
		return migration{}, fmt.Errorf("db: migrate: %s/%s is empty", source.Owner, name)
	}
	header, err := parseHeader(string(body))
	if err != nil {
		return migration{}, fmt.Errorf("db: migrate: %s/%s: %w", source.Owner, name, err)
	}
	return migration{
		migrationID:     migrationID{owner: source.Owner, version: version},
		migrationHeader: header,
		name:            name, sql: string(body), checksum: fmt.Sprintf("%x", sha256.Sum256(body)),
	}, nil
}

// A declared floor exempts applied history. Floors past the source's head are
// checked against pending files after ledger reading, before any file executes.
func checkSourceRules(source MigrationSource, owned []migration) error {
	head, floor := owned[len(owned)-1].version, source.RulesFrom
	declared := floor <= head+1
	for i, m := range owned {
		owned[i].rulesFrom, owned[i].head = floor, head
		if m.version < floor && declared {
			continue
		}
		if !declared {
			continue // checkPendingGuards knows what this database applied.
		}
		if err := checkRules(m); err != nil {
			return fmt.Errorf("db: migrate: %s/%s: %w", source.Owner, m.name, err)
		}
	}
	return nil
}

func readAdoptions(source MigrationSource, versions map[int64]migration) ([]adoption, error) {
	var adoptions []adoption
	for _, a := range source.Adopts {
		if !migrationOwner.MatchString(a.Owner) || a.Owner == source.Owner || len(a.Versions) == 0 {
			return nil, fmt.Errorf("db: migrate: %s adopts from an invalid owner %q", source.Owner, a.Owner)
		}
		for _, version := range a.Versions {
			m, ok := versions[version]
			if !ok {
				return nil, fmt.Errorf("db: migrate: %s adopts version %d from %s and has no such file", source.Owner, version, a.Owner)
			}
			adoptions = append(adoptions, adoption{migration: m, from: a.Owner})
		}
	}
	return adoptions, nil
}

// checkPendingGuards is the rule table's second pass, over the files this database
// has not applied yet, and it exists for one case: a source whose floor is past its
// own head. Such a floor says nothing about what was applied — no release of that
// source has a file up there — so it excuses no file, and every file of that source
// is judged like the file of a source that declared no floor at all. It cannot be
// judged with the rest of the guard, before the connection: which of those files is
// pending is a fact of this ledger, and the files below a real floor are real
// history, which is exactly what the rule table must not refuse.
//
// It runs over every pending file before any of them is planned or applied, so the
// promise the first pass keeps still holds: a file this run refuses does not let an
// earlier file of the same owner change the schema.
func checkPendingGuards(pending []migration) error {
	for _, m := range pending {
		if m.rulesFrom <= m.head+1 {
			continue // judged from the file's own text, with the floor honoured
		}
		if err := checkRules(m); err != nil {
			return fmt.Errorf("%s/%s: %w — and the RulesFrom %d this source declares is past its own highest version %d: a floor excuses files some installation already applied, and no release of this source has a file at version %d to have applied, so it excuses nothing",
				m.owner, m.name, err, m.rulesFrom, m.head, m.head+1)
		}
	}
	return nil
}
