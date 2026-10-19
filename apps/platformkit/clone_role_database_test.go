package main

import (
	"database/sql"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestARoleGrantedInACopyConnectsToThatCopy(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	owner := dbtest.Open(t, cfg.Database.MigrateURL)
	wantDatabase := current(t, owner, "SELECT current_database()")
	role, roleURL := dbtest.Role(t, owner, "SELECT ON TABLE tasks")
	var database string
	t.Cleanup(func() {
		if database == wantDatabase {
			return
		}
		for _, statement := range []string{"DROP OWNED BY " + identify(role), "DROP ROLE IF EXISTS " + identify(role)} {
			if _, err := owner.Exec(statement); err != nil {
				t.Errorf("remove the role granted in this copy: %v", err)
			}
		}
	})
	reader := dbtest.Open(t, roleURL)
	var schema sql.NullString
	if err := reader.QueryRowContext(t.Context(), "SELECT current_database(), current_schema()").Scan(&database, &schema); err != nil {
		t.Fatal(err)
	}
	if database != wantDatabase {
		t.Errorf("the role granted in copy %s connects to database %s", wantDatabase, database)
	}
	if want := current(t, owner, "SELECT current_schema()"); !schema.Valid || schema.String != want {
		t.Errorf("the role granted in schema %s resolves schema %v", want, schema)
	}
}

func TestARoleGrantedInACopyIsRemovedWhenTheCopyEnds(t *testing.T) {
	var role string
	t.Run("the copy holds a role", func(t *testing.T) {
		path, cfg := configure(t)
		install(t, path)
		owner := dbtest.Open(t, cfg.Database.MigrateURL)
		role, _ = dbtest.Role(t, owner, "SELECT ON TABLE tasks")
	})
	if role == "" {
		t.Fatal("the copy was not granted a role")
	}
	_, cfg := configureFresh(t)
	owner := dbtest.Open(t, cfg.Database.MigrateURL)
	t.Cleanup(func() {
		if _, err := owner.Exec("DROP ROLE IF EXISTS " + identify(role)); err != nil {
			t.Errorf("remove the copy's remaining role: %v", err)
		}
	})
	var remains bool
	if err := owner.QueryRowContext(t.Context(), "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)", role).Scan(&remains); err != nil {
		t.Fatal(err)
	}
	if remains {
		t.Errorf("the copy ended but its cluster role %s remains", role)
	}
}
