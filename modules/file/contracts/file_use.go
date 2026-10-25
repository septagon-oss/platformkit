package contracts

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
)

// FileUse is one live use as it is stored: the file, the field of the record
// that shows it, and the locale that body is written in.
type FileUse struct {
	crud.Base
	FileID uuid.UUID `json:"fileId" gorm:"column:file_id;type:uuid;not null" format:"uuid" doc:"The file used" readOnly:"true"`
	Module string    `json:"module" gorm:"type:varchar(32);not null" maxLength:"32" doc:"The module that shows it" readOnly:"true"`
	Entity string    `json:"entity" gorm:"type:varchar(64);not null" maxLength:"64" doc:"The record's entity" readOnly:"true"`
	Record uuid.UUID `json:"record" gorm:"type:uuid;not null" format:"uuid" doc:"The record that shows it" readOnly:"true"`
	Field  string    `json:"field" gorm:"type:varchar(64);not null" maxLength:"64" doc:"The field of that record" readOnly:"true"`
	Locale string    `json:"locale" gorm:"type:varchar(35);not null" maxLength:"35" doc:"The locale of the body that names it" readOnly:"true"`
	// Public says the record this row belongs to was showing the file to somebody
	// who has not signed in. Two commands write it: SetShown, called by the module
	// that owns that record's public face, and SetUsesShown, which files a new use
	// with the audience the record already asserts. The row asserts its own half of
	// the answer: a stranger may read the file when its own visibility says so or
	// some live use row of it says true here, which is the question Open asks at the
	// public door. See migrations/000056.
	Public bool `json:"public" gorm:"not null;default:false" doc:"Shown to anonymous readers" readOnly:"true"`
}

// TableName pins the table, so the entity and migrations/000045 agree.
func (FileUse) TableName() string { return "file_uses" }

// Validate is the same shape file_uses CHECKs, in Go and before the write: an
// empty module, entity, field or locale names no use, and a field that is not
// lower-case would be a second spelling of a field that already exists.
func (u *FileUse) Validate(ctx context.Context) error {
	switch {
	case u.FileID == uuid.Nil:
		return fmt.Errorf("a use names the file it is a use of")
	case u.Module == "" || u.Entity == "":
		return fmt.Errorf("a use names the module and entity that show the file")
	case u.Record == uuid.Nil:
		return fmt.Errorf("a use names the record that shows the file")
	case u.Field == "" || u.Field != strings.ToLower(u.Field):
		return fmt.Errorf("a use names a field, in lower case")
	case u.Locale == "":
		return fmt.Errorf("a use names the locale of the body that names the file")
	}
	return nil
}

// Use names the one thing that can reference a file: a field of one record, in
// one locale. It is SetUses' subject and nothing else's: it carries no verdict
// about whether the file may be used, which is the transaction's, and no
// visibility, which is the record's audience and not the writer's claim.
type Use struct {
	Module, Entity string
	Record         uuid.UUID
	Field, Locale  string
}

// UseRow is one live use as the details panel reads it: the use, and the file it
// belongs to, so a list of a file's uses needs no second read.
type UseRow struct {
	Use
	ID        uuid.UUID `json:"-" doc:"The use row's own id"`
	CreatedAt time.Time `json:"createdAt" doc:"When this use began"`
}

// DistinctRecords is how many records these uses belong to: the count Delete
// names when it refuses, and the count a details panel would show. It is a
// function and not a SQL GROUP BY because the fake has to give the same answer as
// the database for the same rows — the count is one decision, made in one place,
// and a second implementation of it in either language is where the two
// implementations would start to disagree about what one page shows.
func DistinctRecords(rows []UseRow) int {
	seen := map[Use]struct{}{}
	for _, row := range rows {
		seen[Use{Module: row.Module, Entity: row.Entity, Record: row.Record}] = struct{}{}
	}
	return len(seen)
}

// CollapseRefs is what a body's references mean as a set of uses: the ids in the
// order the document shows them, with a file referenced twice by one field used
// once. Document order is kept because the details panel lists what a record
// shows in the order a reader meets it.
func CollapseRefs(refs []uuid.UUID) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(refs))
	for _, id := range refs {
		if id == uuid.Nil || slices.Contains(out, id) {
			continue
		}
		out = append(out, id)
	}
	return out
}

// DiffUses is the rewrite decision: given the ids a field used and the ids it
// uses now, which rows to write and which to end. Both sides arrive already
// collapsed. It is here, and not in either implementation, because the SQL
// service and the fake must not hold two answers to one question.
func DiffUses(existing, wanted []uuid.UUID) (add, remove []uuid.UUID) {
	for _, id := range wanted {
		if !slices.Contains(existing, id) {
			add = append(add, id)
		}
	}
	for _, id := range existing {
		if !slices.Contains(wanted, id) {
			remove = append(remove, id)
		}
	}
	return add, remove
}

// ShownUses is SetUses for the one caller that knows the record filing these uses
// has a public face. It is a separate interface rather than a fifth method on
// Service because a deployment that wired its own Service before this command
// existed is not broken by a capability it never asked for: the one caller that
// wants it asserts it at the call, and refuses the write when the Service it was
// wired cannot answer. See Service.SetUses for the rules it does not change and
// Service.SetShown for what public means.
//
// A row this call files carries public = true, and that row is itself the audience:
// a published page that gains an image in an ordinary edit gains public bytes in
// that write, at the door, without waiting for the next lifecycle move. The file's
// own visibility is left as it stands — writing that row is a lock on the file held
// to the end of the save that took it, and two records naming two shared pictures in
// each other's order deadlock on exactly that, which is what UsesOrder exists for.
// Rows it keeps are left alone: the audience of an existing use
// stays SetShown's to change, and this command came to file new uses, not to
// re-assert old ones. public false is SetUses exactly as it is, which is what
// every command that does not know the answer calls.
type ShownUses interface {
	SetUsesShown(ctx context.Context, tx db.Tx[db.Tenant], use Use, refs []uuid.UUID, public bool) ([]uuid.UUID, error)
}

// UsesOrder is the ledger's promise that the file rows one save touches are locked
// in one pass, in the ledger's own ascending order, whatever order the save names
// them in. It is asked by the one caller that can know a whole save at once — the
// richtext door, which sees every field of the body before it files any of them —
// and it is optional in the way ShownUses is: a Service that answers nothing of the
// kind keeps filing one field at a time, which is the order that can collide.
//
// The collision it prevents is not a race. A save that files a use of one picture in
// its body and another in its summary locks two file rows, in the order the document
// holds them; a second record naming the same two pictures the other way round then
// holds one row and waits for the other while the first waits for it, and Postgres
// ends the deadlock by refusing one of the two saves — a person's write, lost to a
// wait they can neither see nor fix. One ascending pass over everything the save will
// touch makes the second save wait for the first instead, which is a slow save.
//
// OrderUses locks and decides nothing else: it writes no row, publishes no event and
// refuses nothing. The rewrite itself stays SetUses' and SetUsesShown's, and its own
// locks are unchanged — what this pass buys is that those later locks are already
// held, since Postgres grants a row a transaction has locked again for nothing.
// Locking strong and once is the rule this keeps; nothing here takes a row weak and
// upgrades it later, which is what two rewrites of one file deadlock on.
type UsesOrder interface {
	// OrderUses walks the files refs names, together with every file owner's record
	// already shows — the second half is what makes an edit that drops a picture join
	// the same pass — in one ascending FOR UPDATE pass. Files a caller has no right to
	// see are not there to lock, and locking nothing is not an error: the tenant scope
	// answers which rows exist, as it does for every other read here.
	OrderUses(ctx context.Context, tx db.Tx[db.Tenant], owner Use, refs []uuid.UUID) error
}
