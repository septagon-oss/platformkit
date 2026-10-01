package main

import (
	"slices"
	"testing"
	"time"
)

// TestTheReferenceApplicationSchedulesTheSweepForAConfiguredClass is the one
// link between a config file and bytes actually disappearing that no other test
// closes: `files.retention` names a class, the composition hands the table to
// the module with the tenant lister beside it, and the module schedules the job
// that walks the tenants. Hand the table to nothing and the operator's policy is
// a comment in a config file; hand it without the lister and `file.Module`
// panics, which is the same failure one level louder.
//
// The reference application ships the table empty, so the first half is not
// decoration: an installation that never priced a class runs no sweep at all
// rather than one that deletes nothing while looking like it does.
func TestTheReferenceApplicationSchedulesTheSweepForAConfiguredClass(t *testing.T) {
	_, cfg := configure(t)

	if names := jobNames(compose(cfg)); slices.Contains(names, "file-retention") {
		t.Errorf("with files.retention empty the composition schedules %v; a policy nobody configured is not a job", names)
	}

	cfg.Files.Retention = map[string]time.Duration{"chat_photo": 8760 * time.Hour}
	if names := jobNames(compose(cfg)); !slices.Contains(names, "file-retention") {
		t.Errorf("with files.retention naming chat_photo the composition schedules %v, want file-retention beside it", names)
	}
}

// jobNames is every periodic job the composed application would run, named.
func jobNames(c composition) []string {
	var names []string
	for _, m := range c.modules {
		for _, job := range m.Jobs {
			names = append(names, job.Name)
		}
	}
	slices.Sort(names)
	return names
}
