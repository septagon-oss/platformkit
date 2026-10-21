package filetest

import (
	"bytes"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// These steps run under setUses' mutex, so validation and the complete rewrite
// see one ledger. They never acquire or release the lock themselves.

// validateUseFiles refuses the whole rewrite before any of it is written: nothing is
// stored until every id the new set names has been found. That is the whole of the
// refusal's promise — a body that names one gone file records no uses at all, rather
// than the uses of the files that happen to still be there. Ids this rewrite is
// dropping are not asked: they may be gone, and the rows that name them are what the
// write is here to remove.
func (f *Fake) validateUseFiles(wanted []uuid.UUID) error {
	for _, id := range wanted {
		if _, ok := f.rows[id]; !ok {
			return fmt.Errorf("%w: %s is not a file of this tenant", crud.ErrNotFound, id)
		}
	}
	return nil
}

// usedFiles answers "what does this field show" out of the fake's own ledger, which is
// the from side of contracts.DiffUses: the ids currently filed for this tenant and this
// exact module/entity/record/field/locale, in no particular order.
func (f *Fake) usedFiles(scope contracts.Scope, use contracts.Use) []uuid.UUID {
	var from []uuid.UUID
	for _, row := range f.uses {
		if row.TenantID == scope.TenantID() && row.Module == use.Module && row.Entity == use.Entity &&
			row.Record == use.Record && row.Field == use.Field && row.Locale == use.Locale {
			from = append(from, row.FileID)
		}
	}
	return from
}

// useAudiences snapshots who may read each file this rewrite could change, and returns
// nothing when no audience can move: the question is only asked when a public audience
// is being filed or a row that asserted one is being ended. The caller holds f.mu.
func (f *Fake) useAudiences(wanted, remove []uuid.UUID, public bool) map[uuid.UUID]bool {
	var was map[uuid.UUID]bool
	retract := contracts.CollapseRefs(remove)
	if public || len(retract) > 0 {
		was = map[uuid.UUID]bool{}
		for _, id := range append(append([]uuid.UUID{}, wanted...), remove...) {
			was[id] = f.readsPublicly(id)
		}
	}
	return was
}

// rewriteUses puts the ledger down in its new state: the rows this field held are
// dropped, one row per added file is filed under the audience the record asserts, and
// what is left is ordered by file so the fake lists what the SQL's ascending sweep lists.
// The caller holds f.mu.
func (f *Fake) rewriteUses(scope contracts.Scope, use contracts.Use, add, remove []uuid.UUID, public bool) {
	kept := f.uses[:0]
	for _, row := range f.uses {
		if row.TenantID == scope.TenantID() && slices.Contains(remove, row.FileID) &&
			row.Module == use.Module && row.Entity == use.Entity &&
			row.Record == use.Record && row.Field == use.Field && row.Locale == use.Locale {
			continue
		}
		kept = append(kept, row)
	}
	for _, id := range add {
		kept = append(kept, contracts.FileUse{
			Base:   crud.Base{ID: uuid.New(), TenantID: scope.TenantID(), CreatedAt: db.Now(), UpdatedAt: db.Now()},
			FileID: id, Module: use.Module, Entity: use.Entity, Record: use.Record, Field: use.Field, Locale: use.Locale,
			Public: public,
		})
	}
	slices.SortFunc(kept, func(a, b contracts.FileUse) int { return bytes.Compare(a.FileID[:], b.FileID[:]) })
	f.uses = kept
	for _, id := range add {
		// Shown again, so the grace period is over — the same arm internal.Service
		// writes with its UPDATE, run against the row this fake keeps in memory.
		if row, ok := f.rows[id]; ok {
			row.UnusedSince = nil
			f.rows[id] = row
		}
	}
}

// startUnused returns the removed ids no other use of this tenant still holds, and is the
// only place this fake decides a file has gone unused. The caller holds f.mu.
func (f *Fake) startUnused(scope contracts.Scope, remove []uuid.UUID) []uuid.UUID {
	var unused []uuid.UUID
	for _, id := range remove {
		still := false
		for _, row := range f.uses {
			if row.FileID == id && row.TenantID == scope.TenantID() {
				still = true
			}
		}
		if !still {
			unused = append(unused, id)
			if row, ok := f.rows[id]; ok && row.DeletedAt == nil && row.Width > 0 {
				// The grace period starts at the moment the last use went, on the
				// row and not on the copy: the grid's unused filter reads this
				// column, and a stamp that vanished with the local was the fake
				// disagreeing with the row-level policy the SQL service writes.
				at := db.Now()
				row.UnusedSince = &at
				f.rows[id] = row
			}
		}
	}
	return unused
}
