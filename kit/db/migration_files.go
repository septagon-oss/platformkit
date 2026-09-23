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
			versions[version] = m
			owned = append(owned, m)
		}
		if len(owned) == 0 {
			return nil, nil, fmt.Errorf("db: migrate: %s has no migrations at its root; use fs.Sub for an embedded directory", source.Owner)
		}
		slices.SortFunc(owned, func(a, b migration) int { return cmp.Compare(a.version, b.version) })
		// The guard applies from the version the owner says is guarded, which is
		// 0 — every file — for a source with nothing applied yet. A file below
		// the floor cannot carry a marker: that would change applied bytes.
		//
		// A floor is a claim about history: every version under it was applied
		// somewhere, which is why refusing one would stop an installation rather than
		// save one. The history a source can point at ends at its own highest file, so
		// a number past that head plus one claims nothing that exists — it is the rule
		// table switched off for versions nobody has written yet, the opposite of what
		// the field is for. Such a floor excuses no file. It is not refused here: a
		// source may carry one innocently (a floor copied from another owner, a version
		// scheme that skipped), and what the operator can act on is the file. The files
		// it would have excused are judged by the same rule table at
		// checkPendingGuards, which is where the ledger says which of them this database
		// still has pending — before any of them runs, so nothing of the source is
		// applied by a run that a later file of it refuses.
		head, floor := owned[len(owned)-1].version, source.RulesFrom
		declared := floor <= head+1
		for i, m := range owned {
			owned[i].rulesFrom, owned[i].head = floor, head
			if m.version < floor && declared {
				continue
			}
			if !declared {
				continue // checkPendingGuards, which knows what this database applied
			}
			if err := checkRules(m); err != nil {
				return nil, nil, fmt.Errorf("db: migrate: %s/%s: %w", source.Owner, m.name, err)
			}
		}
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
