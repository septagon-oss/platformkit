package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/modules/history/contracts"
)

// table is the one table this store writes, named here and in the migration.
const table = "history_versions"

// useModule and useEntity spell a version's own entry in the file ledger. A
// retained version holds the pictures its body shows exactly as a live body does,
// and holds them under its own row's id: the record it describes may have dropped
// the image long ago, and it is this version, not that record, that entitles the
// file to stand.
const (
	useModule = "history"
	useEntity = "version"
)

// pruneBatch is how many versions one DELETE removes. A sweep that deleted a
// tenant's whole year in one statement would hold its locks for the length of that
// statement, over rows that are — by the rule that selected them — versions nobody
// has read for the length of the retention period.
const pruneBatch = 500

// Service is the version store. What it cannot make for itself is the ledger that
// answers "is anything showing this file", which arrives as the file module's port.
type Service struct {
	uses rest.FileUses
}

// NewService returns the store. module.go constructs it, and nothing else does.
func NewService(uses rest.FileUses) *Service {
	if uses == nil {
		uses = rest.RecordNoUses{}
	}
	return &Service{uses: uses}
}

var _ contracts.Service = (*Service)(nil)

// Record writes one version and files what its bodies show.
//
// The tenant comes from the transaction and never from the input: the input names
// whose record this is, and the transaction names whose tenant may say so, which is
// the whole of how a version of another tenant's record cannot be written even by a
// caller holding a real record id. The document is stored as it arrived — the bytes
// the write committed — because a version that normalised anything on the way in
// would be a version a restore could not be trusted to replay.
//
// A number this series already holds is refused as contracts.ErrDuplicateNumber and
// the caller's whole write rolls back with it: two versions cannot be one write, and
// a store that renumbered to fit would be a store whose numbers mean nothing.
func (s *Service) Record(ctx context.Context, tx db.Tx[db.Tenant], in contracts.Input) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.DB().WithContext(ctx).Raw(
		"INSERT INTO "+table+
			" (tenant_id, module, entity, record_id, locale, version, fields, changed, reason, refs, actor, committed_at)"+
			" VALUES (?, ?, ?, ?, ?, ?, ?::jsonb, ?::jsonb, ?, ?::jsonb, ?, ?) RETURNING id",
		db.TenantOf(tx).ID, in.Module, in.Entity, in.Record, in.Locale, in.Version,
		string(in.Fields), changedJSON(in.Changed), in.Reason, refsJSON(in.Refs),
		nilIfNil(in.Actor), in.At.UTC()).Scan(&id).Error
	if err != nil {
		if strings.Contains(err.Error(), "history_versions_number") {
			return uuid.Nil, fmt.Errorf("%w: %s/%s %s version %d", contracts.ErrDuplicateNumber,
				in.Module, in.Entity, in.Record, in.Version)
		}
		return uuid.Nil, fmt.Errorf("history: keep version %d of %s/%s %s: %w",
			in.Version, in.Module, in.Entity, in.Record, err)
	}
	return id, s.file(ctx, tx, id, in)
}

// file records what this version shows, so the file a version renders is a file in
// use. What the file module refuses for a file that is not this tenant's arrives
// here as this write's own refusal: a version whose body names somebody else's
// picture is not a version worth having, and half of it is worse than none of it.
func (s *Service) file(ctx context.Context, tx db.Tx[db.Tenant], version uuid.UUID, in contracts.Input) error {
	for _, ref := range in.Refs {
		if ref.Field == "" {
			continue
		}
		if err := s.uses.SetUses(ctx, tx, rest.UsesInput{
			Module: useModule, Entity: useEntity, Record: version, Field: ref.Field, Locale: in.Locale,
			Files: ref.Files,
		}); err != nil {
			return fmt.Errorf("history: version %s shows %s: %w", version, ref.Field, err)
		}
	}
	return nil
}

// selectVersion is the one read of a version's columns; every reader here asks for
// the same list, so a column that arrives is a column every reader answers.
const selectVersion = "SELECT id, module, entity, record_id, locale, version, fields, changed, reason, actor, committed_at FROM " + table

// Page answers one record's series in one locale, newest first, at most Limit of
// them. No WHERE names a tenant, because a WHERE can be forgotten and a policy
// cannot: row-level security is the tenant boundary here, as it is everywhere.
func (s *Service) Page(ctx context.Context, tx db.Tx[db.Tenant], q contracts.Query) ([]contracts.Version, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 20
	}
	out, err := versions(ctx, tx, selectVersion+
		" WHERE module = ? AND entity = ? AND record_id = ? AND locale = ? AND version < ?"+
		" ORDER BY version DESC LIMIT ?",
		q.Module, q.Entity, q.Record, q.Locale, beforeOrMax(q.Before))
	if err != nil {
		return nil, fmt.Errorf("history: read the versions of %s/%s %s: %w", q.Module, q.Entity, q.Record, err)
	}
	return out, nil
}

// Version answers one stored version, and crud.ErrNotFound — the answer every
// other door gives a foreign or absent row — when this tenant holds none.
func (s *Service) Version(ctx context.Context, tx db.Tx[db.Tenant], key contracts.Key) (contracts.Version, error) {
	out, err := versions(ctx, tx, selectVersion+
		" WHERE module = ? AND entity = ? AND record_id = ? AND locale = ? AND version = ?",
		key.Module, key.Entity, key.Record, key.Locale, key.Version)
	if err != nil {
		return contracts.Version{}, fmt.Errorf("history: read version %d of %s/%s %s: %w",
			key.Version, key.Module, key.Entity, key.Record, err)
	}
	if len(out) == 0 {
		return contracts.Version{}, fmt.Errorf("%w: no version %d of %s/%s %s in %s",
			crud.ErrNotFound, key.Version, key.Module, key.Entity, key.Record, key.Locale)
	}
	return out[0], nil
}

// Prune removes the versions this tenant's rule no longer keeps and ends the uses
// of the files they were holding.
//
// The keeping is asked of contracts.Rule.Keep and nowhere else: SQL is asked for the
// rank within the series and the instant the version was written, which is all that
// decision reads, because a second statement of the ninety-days-or-fifty rule would
// be a second rule, and the two would eventually disagree about whose versions go.
//
// A version and the pictures it held part company in the same transaction, in both
// directions: no sweep leaves a file with no holder, and none releases a file a
// standing version still shows.
func (s *Service) Prune(ctx context.Context, tx db.Tx[db.Tenant], rule contracts.Rule, now time.Time) (int, error) {
	removed := 0
	for {
		doomed, err := candidates(ctx, tx, rule, now)
		if err != nil {
			return removed, err
		}
		if len(doomed) == 0 {
			return removed, nil
		}
		ids := make([]uuid.UUID, 0, len(doomed))
		for _, d := range doomed {
			ids = append(ids, d.id)
		}
		res := tx.DB().WithContext(ctx).Exec("DELETE FROM "+table+" WHERE id IN ?", pq.Array(ids))
		if res.Error != nil {
			return removed, fmt.Errorf("history: remove the versions retention released: %w", res.Error)
		}
		for _, d := range doomed {
			for _, field := range d.refFields() {
				if err := s.uses.SetUses(ctx, tx, rest.UsesInput{
					Module: useModule, Entity: useEntity, Record: d.id, Field: field, Locale: d.locale,
				}); err != nil {
					return removed, fmt.Errorf("history: release the files version %s held: %w", d.id, err)
				}
			}
		}
		removed += int(res.RowsAffected)
		if len(doomed) < pruneBatch {
			return removed, nil
		}
	}
}

// candidates answers the versions of this tenant's that the rule keeps neither,
// ranked in SQL — ROW_NUMBER over one series, which is what makes the ranking
// correct across a table larger than one transaction's memory — and decided in Go.
// The scan is bounded by a batch times twenty versions, so a tenant whose whole
// history stands is read a page of at a time by the caller's loop, which takes the
// ones it removes out from under the next page.
func candidates(ctx context.Context, tx db.Tx[db.Tenant], rule contracts.Rule, now time.Time) ([]doomed, error) {
	rank := 0
	var series string
	var out []doomed
	rows, err := tx.DB().WithContext(ctx).Raw(
		"SELECT id, locale, refs, committed_at, series FROM (" +
			" SELECT id, locale, refs, committed_at," +
			" (module || '/' || entity || '/' || record_id || '/' || locale) AS series," +
			" ROW_NUMBER() OVER (PARTITION BY module, entity, record_id, locale ORDER BY version DESC) AS ranked" +
			" FROM " + table + ") AS numbered" +
			" ORDER BY series, ranked").Rows()
	if err != nil {
		return nil, fmt.Errorf("history: read the versions retention is deciding between: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id               uuid.UUID
			locale, refs, nm string
			at               time.Time
		)
		if err := rows.Scan(&id, &locale, &refs, &at, &nm); err != nil {
			return nil, fmt.Errorf("history: read a version retention is deciding between: %w", err)
		}
		if nm != series {
			series, rank = nm, 0
		}
		rank++
		if rule.Keep(rank, at, now) {
			continue
		}
		out = append(out, doomed{id: id, locale: locale, refs: refs})
		if len(out) >= pruneBatch {
			return out, nil
		}
	}
	return out, rows.Err()
}

// doomed is one version the rule released: its row, its language, and the fields
// whose files it was holding.
type doomed struct {
	id     uuid.UUID
	locale string
	refs   string
}

// refFields is what this version filed uses under, read back out of the row it was
// stored in: the sweep ends exactly the uses the version wrote, which is the whole
// reason the version's references are kept rather than re-parsed at sweep time.
func (d doomed) refFields() []string {
	var refs []struct {
		Field string `json:"field"`
	}
	if err := json.Unmarshal([]byte(d.refs), &refs); err != nil {
		return nil
	}
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if ref.Field != "" {
			out = append(out, ref.Field)
		}
	}
	return out
}

// versions runs one of the reads above and decodes what it answers.
func versions(ctx context.Context, tx db.Tx[db.Tenant], query string, args ...any) ([]contracts.Version, error) {
	rows, err := tx.DB().WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []contracts.Version
	for rows.Next() {
		var (
			v        contracts.Version
			changed  string
			actor    *uuid.UUID
			fields   []byte
			versions int64
		)
		if err := rows.Scan(&v.ID, &v.Module, &v.Entity, &v.Record, &v.Locale, &versions,
			&fields, &changed, &v.Reason, &actor, &v.At); err != nil {
			return nil, err
		}
		v.Version = versions
		v.Fields = fields
		v.Actor = deref(actor)
		if err := json.Unmarshal([]byte(changed), &v.Changed); err != nil {
			return nil, fmt.Errorf("history: version %d holds a field list that is not a list: %w", v.Version, err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func changedJSON(changed []string) string {
	if len(changed) == 0 {
		return "[]"
	}
	encoded, err := json.Marshal(changed)
	if err != nil {
		return "[]"
	}
	return string(encoded)
}

// refsJSON is the version's own list of what it shows, stored for the sweep that
// reads it back — see doomed.refFields.
func refsJSON(refs []contracts.Ref) string {
	type ref struct {
		Field string      `json:"field"`
		Files []uuid.UUID `json:"files"`
	}
	out := make([]ref, 0, len(refs))
	for _, r := range refs {
		out = append(out, ref{Field: r.Field, Files: r.Files})
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return "[]"
	}
	return string(encoded)
}

func nilIfNil(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}

func deref(id *uuid.UUID) uuid.UUID {
	if id == nil {
		return uuid.Nil
	}
	return *id
}

// beforeOrMax is the "everything older" end of a page: a first page names no
// version to start below, and asking for the versions below the largest number a
// bigint holds says so without a second query shape.
func beforeOrMax(before int64) int64 {
	if before > 0 {
		return before
	}
	return 1<<62 - 1
}
