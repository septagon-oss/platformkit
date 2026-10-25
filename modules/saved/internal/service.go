package internal

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/saved/contracts"
)

// Service keeps each person's retained copies in saved_items. It owns no source
// data: the declaration set names one Source per (module, kind), and every
// capture, availability check and destination comes from that source inside the
// caller's transaction, so the source answers with its own row locks and grants.
//
// A copy is private to one person by construction — every statement carries
// person_id, and row-level security supplies the independent tenant boundary.
type Service struct {
	// Access resolves the acting member and their current grant in the caller's
	// transaction. It is a port rather than a query: membership and grants have
	// other owners, and a cached request grant cannot answer for a wait.
	Access contracts.Access

	// Savables is the composition's declaration set, frozen at wiring. A source
	// that is not declared is ErrUnavailable: saving is only ever possible for
	// something a composition said how to capture.
	Savables []contracts.Savable

	// Clock is the instant a snapshot is taken at. NewService leaves it nil, which
	// is the kernel's process clock; the module's boundary requires a Clock, so a
	// composed service never runs on this default.
	Clock contracts.Clock
}

// now is the instant this stamping happens at, from whoever the composition put
// there. The kernel's own clock is the fallback for a service built outside a
// composition, which is a test's own construction and not a composed answer.
func (s *Service) now() time.Time {
	if s.Clock == nil {
		return db.Now()
	}
	return s.Clock()
}

// NewService returns the SQL adapter over the declaration set. Access must not be
// nil for anything but a read-only composition: a service with no authority port
// refuses every command rather than guessing who is asking. The returned service
// stamps with the kernel's process clock until its owner sets Clock, which a
// composition does through the module's Deps.
func NewService(access contracts.Access, savables []contracts.Savable) *Service {
	return &Service{Access: access, Savables: slices.Clone(savables)}
}

var _ contracts.Service = (*Service)(nil)

// row is one saved_items tuple. The snapshot is flat here and nested in the
// contract because the columns are what the unique index and the two page
// indexes address, while a caller reads one object.
type row struct {
	entity.Base
	PersonID      uuid.UUID  `gorm:"column:person_id"`
	SourceModule  string     `gorm:"column:source_module"`
	SourceKind    string     `gorm:"column:source_kind"`
	SourceKey     string     `gorm:"column:source_key"`
	Revision      int64      `gorm:"column:revision"`
	Title         string     `gorm:"column:title"`
	Locale        string     `gorm:"column:locale"`
	Version       string     `gorm:"column:source_version"`
	Values        string     `gorm:"column:values"`
	FileID        *uuid.UUID `gorm:"column:picture_file_id"`
	SVG           string     `gorm:"column:picture_svg"`
	Digest        string     `gorm:"column:picture_template_digest"`
	ComposedValue string     `gorm:"column:picture_value"`
	CapturedAt    time.Time  `gorm:"column:captured_at"`
}

// TableName names the sole table this module owns.
func (*row) TableName() string { return "saved_items" }

// item is the contract's view of a row. Every read returns a copy: the retained
// values are handed over as fresh bytes, so a caller cannot write through to the
// next read of the same row.
func (r *row) item() contracts.Item {
	// The driver hands a timestamptz back in the session's zone. The copy's own
	// contract is UTC, and a read that answered in another zone would disagree
	// with the write that made it, which is the one thing an immutable snapshot
	// cannot afford.
	stamp, changed, taken := r.CreatedAt.UTC(), r.UpdatedAt.UTC(), r.CapturedAt.UTC()
	image := contracts.Picture{}
	if r.FileID != nil {
		image.FileID = r.FileID.String()
	} else if r.SVG != "" {
		image.SVG = r.SVG
		image.TemplateDigest = r.Digest
		image.Value = json.RawMessage(r.ComposedValue)
	}
	return contracts.Item{
		Base:     entity.Base{ID: r.ID, TenantID: r.TenantID, CreatedAt: stamp, UpdatedAt: changed, DeletedAt: r.DeletedAt, I18N: r.I18N},
		PersonID: r.PersonID.String(),
		Source:   contracts.Ref{Module: r.SourceModule, Kind: r.SourceKind, Key: r.SourceKey},
		Revision: r.Revision,
		Snapshot: contracts.Snapshot{Title: r.Title, Locale: r.Locale, SourceVersion: r.Version,
			Values: json.RawMessage(r.Values), Picture: image, CapturedAt: taken},
	}
}

// person is the acting member, resolved afresh in tx for one permission.
func (s *Service) person(ctx context.Context, tx db.Tx[db.Tenant], permission string) (uuid.UUID, error) {
	if s.Access == nil {
		return uuid.Nil, contracts.ErrDenied
	}
	raw, err := s.Access.Current(ctx, tx, permission)
	if err != nil {
		return uuid.Nil, err
	}
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, contracts.ErrDenied
	}
	return id, nil
}

// source answers the declaration the composition froze for this pair.
func (s *Service) source(ref contracts.Ref) (contracts.Source, error) {
	for _, declared := range s.Savables {
		if declared.Module == ref.Module && declared.Kind == ref.Kind && declared.Source != nil {
			return declared.Source, nil
		}
	}
	return nil, contracts.ErrUnavailable
}

// hold takes a transaction-scoped advisory lock over one tenant's one person's
// one source. Find-or-insert cannot be one row's lock: two first saves of the
// same source are two inserts, and the unique index would make one of them an
// error rather than the idempotent success the contract promises. The lock is
// released with the transaction, so a refusal keeps nothing open.
func hold(ctx context.Context, tx db.Tx[db.Tenant], person uuid.UUID, ref contracts.Ref) error {
	// Tenant and person carry no "|", so the key that follows them cannot be read
	// as anything else; Postgres keeps no NUL in text for a delimiter. The tenant
	// is in the key because person ids arrive from another owner's users table and
	// two tenants must not serialize on one another's copies.
	fact := db.TenantOf(tx).ID.String() + "|" + person.String() + "|" + ref.Module + "|" + ref.Kind + "|" + ref.Key
	return tx.DB().Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, fact).Error
}

// prior reads the copy this person already holds of this source, if any.
func (s *Service) prior(tx db.Tx[db.Tenant], person uuid.UUID, ref contracts.Ref) (*row, error) {
	var found row
	err := crud.Classify(tx.DB().Where("person_id = ? AND source_module = ? AND source_kind = ? AND source_key = ?",
		person, ref.Module, ref.Kind, ref.Key).First(&found).Error)
	if errors.Is(err, crud.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &found, nil
}

// Save keeps the first authorized capture and never a later one. A copy that
// already exists is reauthorized, its source is asked again, and the retained
// snapshot answers: the caller's expected version still has to match what the
// source says now, so a duplicate cannot outlive a change it did not see.
func (s *Service) Save(ctx context.Context, tx db.Tx[db.Tenant], in contracts.Save) (contracts.Item, error) {
	person, err := s.person(ctx, tx, contracts.PermissionWrite)
	if err != nil {
		return contracts.Item{}, err
	}
	if err = ValidSave(in); err != nil {
		return contracts.Item{}, err
	}
	source, err := s.source(in.Source)
	if err != nil {
		return contracts.Item{}, err
	}
	if err = hold(ctx, tx, person, in.Source); err != nil {
		return contracts.Item{}, err
	}
	capture, err := source.Capture(ctx, tx, in.Source.Key, in.ExpectedSourceVersion)
	if err != nil {
		return contracts.Item{}, err
	}
	// The source call is a wait, and a grant revoked during it is refused here
	// rather than written: what authorizes this command is the answer of the
	// authority owner after the wait, not the one that let it start.
	if _, err = s.person(ctx, tx, contracts.PermissionWrite); err != nil {
		return contracts.Item{}, err
	}
	// The held copy is read after both waits, not before them. A removal that
	// committed while the source was being asked is the state this command now
	// sees, so it answers with the copy it actually wrote rather than with one
	// that is gone, and the row it returns always exists.
	before, err := s.prior(tx, person, in.Source)
	if err != nil {
		return contracts.Item{}, err
	}
	var held *contracts.Item
	if before != nil {
		earlier := before.item()
		held = &earlier
	}
	snapshot, changed, err := DecideSave(held, in, capture, s.now())
	if err != nil {
		return contracts.Item{}, err
	}
	if !changed {
		return *held, nil
	}
	return s.insert(ctx, tx, person, in.Source, snapshot)
}

// insert writes the one row and its event together; either both commit or neither.
func (s *Service) insert(ctx context.Context, tx db.Tx[db.Tenant], person uuid.UUID, ref contracts.Ref, snapshot contracts.Snapshot) (contracts.Item, error) {
	// The three timestamps a copy carries are equal on creation, and the two the
	// kernel owns are set here rather than left to the driver: gorm would stamp
	// nanoseconds the column cannot hold, and the row a later read returns would
	// then disagree with the one this command answered.
	next := row{Base: entity.Base{ID: uuid.New(), CreatedAt: snapshot.CapturedAt, UpdatedAt: snapshot.CapturedAt}, PersonID: person,
		SourceModule: ref.Module, SourceKind: ref.Kind, SourceKey: ref.Key, Revision: 1,
		Title: snapshot.Title, Locale: snapshot.Locale, Version: snapshot.SourceVersion,
		Values: string(snapshot.Values), SVG: snapshot.Picture.SVG,
		Digest: snapshot.Picture.TemplateDigest, ComposedValue: string(snapshot.Picture.Value),
		CapturedAt: snapshot.CapturedAt}
	if snapshot.Picture.FileID != "" {
		id, err := uuid.Parse(snapshot.Picture.FileID)
		if err != nil {
			return contracts.Item{}, contracts.ErrInvalid
		}
		next.FileID, next.SVG, next.Digest, next.ComposedValue = &id, "", "", ""
	}
	if err := crud.Create(ctx, tx, &next); err != nil {
		return contracts.Item{}, err
	}
	created := next.item()
	if err := events.Publish(ctx, tx, contracts.EventSaved, contracts.Changed{ID: next.ID.String(), Revision: next.Revision}); err != nil {
		return contracts.Item{}, err
	}
	return created, nil
}

// Unsave takes a copy away. A row that is not there — never saved, already
// removed, or somebody else's — is the state the caller asked for and writes
// nothing; a present own row is locked and its quoted revision checked first.
func (s *Service) Unsave(ctx context.Context, tx db.Tx[db.Tenant], in contracts.Unsave) error {
	person, err := s.person(ctx, tx, contracts.PermissionWrite)
	if err != nil {
		return err
	}
	id, err := ItemID(in.ID)
	if err != nil {
		return err
	}
	found, err := crud.GetForUpdate[*row](tx, id)
	if errors.Is(err, crud.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if found.PersonID != person {
		return nil
	}
	current := found.item()
	changed, err := DecideUnsave(&current, in.ExpectedRevision)
	if err != nil || !changed {
		return err
	}
	// Unscoped: a removed copy is gone, not hidden. gorm's DeletedAt convention
	// would leave the row addressable by its own id, which is the one thing a
	// retained copy that was taken back must not be.
	if err := tx.DB().Unscoped().Where("id = ?", id).Delete(&row{}).Error; err != nil {
		return err
	}
	return events.Publish(ctx, tx, contracts.EventUnsaved, contracts.Changed{ID: id.String(), Revision: current.Revision})
}

// own reads one row of this person's. A nil row with no error is the copy not
// being there — never saved, already removed, or somebody else's, which reads the
// same by design. A failed read is not absence and keeps its error: the absence is
// an immutable answer a caller may cache, and a database that did not answer is
// neither.
func (s *Service) own(tx db.Tx[db.Tenant], person uuid.UUID, id uuid.UUID) (*row, error) {
	found, err := crud.Get[*row](tx, id)
	if errors.Is(err, crud.ErrNotFound) || (err == nil && found.PersonID != person) {
		return nil, nil
	}
	return found, err
}

// view adds the live observation to a retained copy. The copy is already read;
// a source that has gone changes the words on it, never its content — including
// a (module, kind) the composition no longer declares, which is the same
// observation as a source whose row was deleted. A read that refused it would
// leave the person unable to see or remove what they saved, which is the one
// thing a copy that outlives its source must not do.
func (s *Service) view(ctx context.Context, tx db.Tx[db.Tenant], item contracts.Item) (contracts.View, error) {
	source, err := s.source(item.Source)
	if err == nil {
		err = source.Check(ctx, tx, item.Source.Key)
	}
	status, err := Availability(err)
	if err != nil {
		return contracts.View{}, err
	}
	return contracts.View{Item: item, Availability: status}, nil
}

func (s *Service) Get(ctx context.Context, tx db.Tx[db.Tenant], raw string) (contracts.View, error) {
	person, err := s.person(ctx, tx, contracts.PermissionRead)
	if err != nil {
		return contracts.View{}, err
	}
	id, err := ItemID(raw)
	if err != nil {
		return contracts.View{}, err
	}
	found, err := s.own(tx, person, id)
	if err != nil {
		return contracts.View{}, err
	}
	if found == nil {
		return contracts.View{}, contracts.ErrNotFound
	}
	return s.view(ctx, tx, found.item())
}

func (s *Service) Find(ctx context.Context, tx db.Tx[db.Tenant], ref contracts.Ref) (contracts.View, error) {
	person, err := s.person(ctx, tx, contracts.PermissionRead)
	if err != nil {
		return contracts.View{}, err
	}
	if err = ValidRef(ref); err != nil {
		return contracts.View{}, err
	}
	found, err := s.prior(tx, person, ref)
	if err != nil {
		return contracts.View{}, err
	}
	if found == nil {
		return contracts.View{}, contracts.ErrNotFound
	}
	return s.view(ctx, tx, found.item())
}

// List answers one page of this person's copies, newest first. Membership,
// module and kind narrow the set before its total is counted and before the page
// is taken, so a foreign row or a filtered-out one consumes neither a slot nor
// the count.
func (s *Service) List(ctx context.Context, tx db.Tx[db.Tenant], q contracts.Query) (contracts.Page, error) {
	person, err := s.person(ctx, tx, contracts.PermissionRead)
	if err != nil {
		return contracts.Page{}, err
	}
	limit, err := PageBounds(q)
	if err != nil {
		return contracts.Page{}, err
	}
	page := tx.DB().Model(&row{}).Where("person_id = ?", person)
	if q.Module != "" {
		page = page.Where("source_module = ?", q.Module)
	}
	if q.Kind != "" {
		page = page.Where("source_kind = ?", q.Kind)
	}
	var total int64
	if err = page.Count(&total).Error; err != nil {
		return contracts.Page{}, err
	}
	var rows []row
	if err = page.Order("captured_at DESC, id DESC").Limit(limit).Offset(q.Offset).Find(&rows).Error; err != nil {
		return contracts.Page{}, err
	}
	out := contracts.Page{Items: []contracts.View{}, Total: total}
	for i := range rows {
		view, err := s.view(ctx, tx, rows[i].item())
		if err != nil {
			return contracts.Page{}, err
		}
		out.Items = append(out.Items, view)
	}
	return out, nil
}

// Open asks the source for a destination every time: a retained copy is a right
// to display what was captured, never a right that outlives the source's own
// authorization.
func (s *Service) Open(ctx context.Context, tx db.Tx[db.Tenant], raw string) (string, error) {
	person, err := s.person(ctx, tx, contracts.PermissionRead)
	if err != nil {
		return "", err
	}
	id, err := ItemID(raw)
	if err != nil {
		return "", err
	}
	found, err := s.own(tx, person, id)
	if err != nil {
		return "", err
	}
	if found == nil {
		return "", contracts.ErrNotFound
	}
	source, err := s.source(found.item().Source)
	if err != nil {
		return "", err
	}
	path, err := source.Open(ctx, tx, found.SourceKey)
	if err != nil {
		return "", err
	}
	return Destination(path)
}
