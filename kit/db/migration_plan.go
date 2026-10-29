package db

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"slices"
)

// groupedByOwner keeps each owner's pending files together and in version order,
// which is how they arrive: readMigrations appends one source at a time, sorted.
// The grouping is what lets a release rule be judged over a whole owner before any
// of its files runs.
func groupedByOwner(pending []migration) [][]migration {
	var groups [][]migration
	for i, m := range pending {
		if i == 0 || pending[i-1].owner != m.owner {
			groups = append(groups, nil)
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], m)
	}
	return groups
}

// planOwner orders one owner's pending release before any of its statements run.
// Contracts require an earlier expansion; installed owners must already hold it.
// A new data drain with schema work behind it waits for the worker; started drains
// and bounded final drains resume here. Fresh owners drain inline.
// See README.md for the migration and backfill lifecycle.
func planOwner(ctx context.Context, conn *sql.Conn, files []migration, history migrationHistory) ([]migration, error) {
	owner := files[0].owner
	// An owner with no history is the exception twice over: nobody is reading its tables,
	// and its own files are the only release they will ever arrive in. Both of the
	// questions below are about *this installation*, and neither has an answer to give on
	// a fresh one. The question `expand=` asks is not one of those, so it is asked
	// whatever the ledger says: see namesAnExpansion.
	fresh := history.latest[owner] == 0
	for i, m := range files {
		if m.phase == phaseContract {
			if err := namesAnExpansion(m, files, fresh); err != nil {
				return nil, err
			}
			if !fresh && !history.applied[migrationID{owner, m.contractOf}] {
				return nil, refusal(refusalMissingExpansion,
					fmt.Sprintf("%s/%s waits for %s of the same owner, which this installation has not applied yet", owner, m.name, partnerFile(files, m.contractOf)),
					"the contract half runs in the release after the expansion it removes")
			}
		}
		if fresh || m.phase != phaseData {
			continue
		}
		progress := drainCursor(ctx, conn, m.migrationID)
		if progress.err != nil {
			return nil, progress.err
		}
		// The drain this run owns: one it found in flight, and the owner's last
		// pending file, which has nothing behind it and a window to bound.
		if progress.started || (i == len(files)-1 && m.windowed()) {
			continue
		}
		slog.WarnContext(ctx, "db: left migrations pending behind a backfill the worker drains",
			"owner", owner, "version", m.version, "remaining", len(files)-i)
		return files[:i], nil
	}
	return files, nil
}

// namesAnExpansion checks that a contract names an earlier expansion in its
// owner's release. Fresh installations validate the file relationship; installed
// owners also require the expansion in their applied history.
func namesAnExpansion(m migration, files []migration, fresh bool) error {
	head := files[len(files)-1].version
	if m.contractOf >= m.version {
		return refusal(refusalMissingExpansion,
			fmt.Sprintf("%s/%s waits for version %d of the same owner, which is not a file before it — this release runs to version %d, and a contract half whose expansion comes after it applies in no order that makes it safe",
				m.owner, m.name, m.contractOf, head),
			"ship the contract half in the release after the expansion it removes, with expand=<that earlier version>")
	}
	if fresh && !listsVersion(files, m.contractOf) {
		return refusal(refusalMissingExpansion,
			fmt.Sprintf("%s/%s waits for version %d of the same owner, which no file of this release is — the versions run to %d and none is %d, so no installation can ever have applied it",
				m.owner, m.name, m.contractOf, head, m.contractOf),
			"expand= names the version of the same owner that added the column this file takes away")
	}
	return nil
}

// listsVersion says whether one of these files carries the version. It is read of the
// pending files because those are the whole of what an owner with no history has.
func listsVersion(files []migration, version int64) bool {
	return slices.ContainsFunc(files, func(m migration) bool { return m.version == version })
}

// partnerFile names the version a contract half waits for as a file, because that
// is what an operator greps for and what a release note lists.
func partnerFile(files []migration, version int64) string {
	for _, m := range files {
		if m.version == version {
			return m.name
		}
	}
	return fmt.Sprintf("version %d", version)
}
