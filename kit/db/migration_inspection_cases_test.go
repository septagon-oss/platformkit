package db

import (
	"testing"
	"testing/fstest"
)

func TestMigrationInspectionCasesShareHistoryDecision(t *testing.T) {
	source := MigrationSource{Owner: "sample", Files: fstest.MapFS{
		"000001_start.up.sql": {Data: []byte("CREATE TABLE sample (id integer)")},
		"000002_more.up.sql":  {Data: []byte("ALTER TABLE sample ADD COLUMN name text")},
	}}
	migrations, adoptions, err := readMigrations([]MigrationSource{source})
	if err != nil {
		t.Fatal(err)
	}
	first := migrations[0]
	second := migrations[1]
	cases := []struct {
		name         string
		applied      []appliedMigration
		pending      int
		incompatible bool
	}{
		{name: "empty history", pending: 2},
		{name: "all applied", applied: []appliedMigration{record(first), record(second)}},
		{name: "one pending", applied: []appliedMigration{record(first)}, pending: 1},
		{name: "changed checksum", applied: []appliedMigration{{owner: first.owner, version: first.version, name: first.name, checksum: "changed"}}, incompatible: true},
		{name: "unknown applied file", applied: []appliedMigration{{owner: first.owner, version: 9, name: "000009_missing.up.sql", checksum: "unknown"}}, incompatible: true},
		{name: "pending file before applied history", applied: []appliedMigration{record(second)}, incompatible: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := compareMigrationHistory(migrations, adoptions, tc.applied)
			if (got.Problem != "") != tc.incompatible {
				t.Fatalf("problem=%q, incompatible=%v", got.Problem, tc.incompatible)
			}
			if len(got.Pending) != tc.pending {
				t.Fatalf("pending=%v, want %d entries", got.Pending, tc.pending)
			}
		})
	}
}

func record(item migration) appliedMigration {
	return appliedMigration{owner: item.owner, version: item.version, name: item.name, checksum: item.checksum}
}
