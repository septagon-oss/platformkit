package main

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestEachRepeatedCopyCanHoldAnIndependentRoleInItsOwnDatabase(t *testing.T) {
	roles := make(map[string]string)
	for index := range 11 {
		adminURL, _ := dbtest.Clone(t)
		owner := dbtest.Open(t, adminURL)
		database := current(t, owner, "SELECT current_database()")
		t.Logf("copy %d: %s", index+1, database)
		role, roleURL := dbtest.Role(t, owner, "SELECT ON TABLE tasks")
		if previous, exists := roles[role]; exists {
			t.Errorf("copies %q and %q share the role %q", previous, database, role)
		}
		roles[role] = database
		reader := dbtest.Open(t, roleURL)
		if got := current(t, reader, "SELECT current_database()"); got != database {
			t.Errorf("copy %d grants in %q but its role connects to %q", index+1, database, got)
		}
	}
}
