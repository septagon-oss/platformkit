// The two doors: the request row, the work the worker does later, and the link.
//
// Everything in this file is the part of ADR 0020 §5 to §7 that needs a row of its own.
// The read (Export) and the irreversible command (Erase) are in service.go and are
// unchanged: a request is a wrapper around them, not a second answer. The split matters
// because the two have different failure rules — a refused erasure writes nothing at all,
// while a failed export has to leave a row that says it failed, and why.
//
// The request row is written by the request, and the bundle is produced by the worker.
// That is not ceremony: walking nine owners is minutes of work on a tenant with rows, and
// a POST that did it inline would time out with the person none the wiser about whether to
// try again. The row is what turns "we are working on it" into something they can read.
package internal

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/subject/contracts"
	"gorm.io/gorm/clause"
)

// requestPath is where a request row lives, and the notice's link is that and nothing
// more: no token, no query, nothing a database read could hand out (ADR 0020 §6). It is
// spelled beside the route that answers it, in internal/handler.go, so the two cannot
// disagree about an address a person clicks.
const requestPath = "/api/v1/subject/exports/"

// walkLimit bounds one tenant's tick. A tenant with more open requests than this has an
// operator with a queue, and the next tick takes the rest — the same bound, for the same
// reason, as modules/task's sweep.
const walkLimit = 50

// RequestExport files the ask and publishes the fact, in one transaction.
func (s *service) RequestExport(ctx context.Context, tx db.Tx[db.Tenant], in contracts.RequestExportInput) (*contracts.Request, error) {
	if len(in.Reason) > contracts.MaxReason {
		return nil, fmt.Errorf("subject: the reason is past %d characters: %w", contracts.MaxReason, crud.ErrInvalid)
	}
	actor, ok := tenancy.ActorFrom(ctx)
	if !ok {
		return nil, fmt.Errorf("subject: an export request names the person who asked: %w", crud.ErrInvalid)
	}
	// The person has to be here. The alternative is a `requested` row for an id that
	// resolves to nobody, which the worker will fail forever and no screen will explain.
	if _, err := s.person(ctx, tx, in.Subject); err != nil {
		return nil, err
	}
	return s.file(ctx, tx, contracts.Request{
		Subject: in.Subject, Requester: actor, Kind: contracts.KindExport,
		State: contracts.StateRequested, Reason: in.Reason,
	})
}

// RequestErase runs the erasure and files its receipt in the same transaction.
//
// The unique index on (tenant, subject) for the erase kind is what makes a redelivery
// answer the first erasure rather than run a second one: the insert loses, the row that
// won is read back with the state it already reached, and nothing is scrubbed again
// (ADR 0020, Consequences: two erasures of one person are one erasure).
func (s *service) RequestErase(ctx context.Context, tx db.Tx[db.Tenant], in contracts.EraseInput) (*contracts.Request, *contracts.Erasure, error) {
	actor, ok := tenancy.ActorFrom(ctx)
	if !ok {
		return nil, nil, fmt.Errorf("subject: an erasure names the person who asked: %w", crud.ErrInvalid)
	}
	row, err := s.file(ctx, tx, contracts.Request{
		Subject: in.Subject, Requester: actor, Kind: contracts.KindErase,
		State: contracts.StateRequested,
	})
	if errors.Is(err, crud.ErrConflict) {
		// Somebody — or this same caller, retried — already asked. The row that exists is
		// the answer if it finished, and the erasure below is the retry if it did not.
		if row, err = s.openErase(ctx, tx, in.Subject); err != nil {
			return nil, nil, err
		}
	} else if err != nil {
		return nil, nil, err
	}
	if row.State == contracts.StateErased {
		return row, nil, nil
	}

	// The confirmation reaches Erase unchanged. It is the command that owns the refusal,
	// because it is the command that reads the row the address is checked against.
	receipt, err := s.Erase(ctx, tx, in)
	if err != nil {
		// A refused erasure writes nothing — including this request row, which the
		// caller's transaction rolls back with everything else. A `failed` row left behind
		// by a rollback would be a record of a thing that never happened.
		return nil, nil, err
	}
	at := s.now()
	if err := tx.DB().WithContext(ctx).Model(&contracts.Request{}).
		Where("id = ? AND revision = ?", row.ID, row.Revision).
		Updates(map[string]any{
			"state": contracts.StateErased, "updated_at": at, "revision": row.Revision + 1,
		}).Error; err != nil {
		return nil, nil, err
	}
	row.State, row.UpdatedAt, row.Revision = contracts.StateErased, at, row.Revision+1
	return row, receipt, nil
}

// openErase reads the erase request one person already has open.
func (s *service) openErase(ctx context.Context, tx db.Tx[db.Tenant], subject uuid.UUID) (*contracts.Request, error) {
	row := &contracts.Request{}
	err := tx.DB().WithContext(ctx).
		Where("subject_id = ? AND kind = ?", subject, contracts.KindErase).First(row).Error
	if err != nil {
		return nil, fmt.Errorf("subject: %w", crud.Classify(err))
	}
	return row, nil
}

// file writes one row and, for an export, the event that puts it in the queue.
func (s *service) file(ctx context.Context, tx db.Tx[db.Tenant], row contracts.Request) (*contracts.Request, error) {
	at := s.now()
	// The row names itself. The column has a database default, but a Create that left the
	// id zero would write the zero uuid and collide with the last request that way rather
	// than in a way anybody could read — and the erase kind's unique index, which is the
	// conflict this function is written to recognise, would never be the first complaint.
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	// The tenant comes from the transaction, never the caller: the same rule kit/crud
	// follows for every entity, and the WITH CHECK of the policy is the second answer.
	row.Tenant = db.TenantOf(tx).ID
	row.CreatedAt, row.UpdatedAt, row.Revision = at, at, 1
	// ON CONFLICT DO NOTHING rather than a violated constraint: a refused INSERT aborts
	// the whole transaction in Postgres, and the erase kind's whole point is what happens
	// after the refusal — the caller reads the row that won. So the insert that changes
	// nothing says so, and the transaction stays open.
	res := tx.DB().WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, fmt.Errorf("subject: %w", crud.ErrConflict)
	}
	if row.Kind == contracts.KindExport {
		// Row and event commit together, so a request nobody queued cannot exist and an
		// event cannot name a row that was rolled back.
		if err := events.Publish(ctx, tx, contracts.EventExportRequested, contracts.ExportRequested{
			Request: row.ID, Subject: row.Subject, Requester: row.Requester, At: at,
		}); err != nil {
			return nil, err
		}
	}
	return &row, nil
}

// Request answers one request row to the person who filed it.
func (s *service) Request(ctx context.Context, tx db.Tx[db.Tenant], request, actor uuid.UUID, operator bool) (*contracts.Request, error) {
	row := &contracts.Request{}
	if err := tx.DB().WithContext(ctx).Where("id = ?", request).First(row).Error; err != nil {
		return nil, fmt.Errorf("subject: %w", crud.Classify(err))
	}
	if !operator && row.Requester != actor {
		return nil, crud.ErrNotFound
	}
	return row, nil
}

// Mint opens the seven-day door onto one bundle.
//
// The caller is either the person who asked or a holder of `subject:export`, and the route
// says which of the two it authenticated, because this command cannot read a grant from
// inside the transaction. What it can and does check is the requester: a request id is not
// a capability, and naming a row is not authorisation to open it. Somebody else's request
// answers not-found, which is the answer that does not confirm the row exists.
func (s *service) Mint(ctx context.Context, tx db.Tx[db.Tenant], request, actor uuid.UUID, operator bool) (*contracts.Link, error) {
	row := &contracts.Request{}
	if err := tx.DB().WithContext(ctx).Where("id = ?", request).First(row).Error; err != nil {
		return nil, fmt.Errorf("subject: %w", crud.Classify(err))
	}
	if !operator && row.Requester != actor {
		return nil, crud.ErrNotFound
	}
	if row.Bundle == uuid.Nil || row.State != contracts.StateReady {
		return nil, fmt.Errorf("subject: that request has no bundle to link to yet: %w", crud.ErrInvalid)
	}
	token, hash, err := contracts.NewToken()
	if err != nil {
		return nil, err
	}
	at := s.now()
	expires := at.Add(contracts.LinkLife)
	grant := contracts.Grant{
		ID: uuid.New(), Tenant: db.TenantOf(tx).ID, Request: row.ID,
		TokenHash: hash, ExpiresAt: expires, CreatedAt: at,
	}
	if err := tx.DB().WithContext(ctx).Create(&grant).Error; err != nil {
		return nil, err
	}
	if err := tx.DB().WithContext(ctx).Model(&contracts.Request{}).
		Where("id = ?", row.ID).UpdateColumns(map[string]any{
		"link_expires_at": expires, "updated_at": at,
	}).Error; err != nil {
		return nil, err
	}
	// The hold moves with the link. A bundle held only to the instant the walk finished
	// would be swept out from under a person who asked for their door on day six.
	if err := s.bundles.Hold(ctx, tx, row.Bundle, expires); err != nil {
		return nil, err
	}
	return &contracts.Link{
		Request: row.ID, Subject: row.Subject, File: row.Bundle,
		Token: token, ExpiresAt: expires,
	}, nil
}

// Redeem spends a token and answers the bundle it points at.
func (s *service) Redeem(ctx context.Context, tx db.Tx[db.Tenant], token string) (*contracts.Link, error) {
	hash := contracts.HashToken(token)
	at := s.now()
	// One statement, so a link cannot be opened twice by two requests that read the row at
	// the same moment: the UPDATE is the read, and the row it took is the answer.
	// Read under a row lock, then spend the row that was read — not the row the pattern
	// matches, which is the same row only until another redeenter gets here first. The lock
	// makes the second caller wait, and then the `redeemed_at IS NULL` below matches nothing
	// and they get the refusal. Without the lock two requests could both read an unspent
	// row, both update it, and both download the bundle on one token.
	var spent struct {
		ID      uuid.UUID
		Request uuid.UUID
	}
	rows, err := tx.DB().WithContext(ctx).Raw(
		`SELECT id, request_id FROM subject_grants
		  WHERE token_hash = ? AND redeemed_at IS NULL AND expires_at > ? FOR UPDATE`,
		hash, at).Rows()
	if err != nil {
		return nil, err
	}
	found := false
	if rows.Next() {
		if err := rows.Scan(&spent.ID, &spent.Request); err != nil {
			rows.Close()
			return nil, err
		}
		found = true
		rows.Close()
	}
	if !found {
		// Unknown, expired and spent: one answer, and no line of it says which.
		return nil, contracts.ErrGrant
	}
	res := tx.DB().WithContext(ctx).Model(&contracts.Grant{}).
		Where("id = ? AND redeemed_at IS NULL", spent.ID).
		UpdateColumns(map[string]any{"redeemed_at": at})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected != 1 {
		return nil, contracts.ErrGrant
	}
	row := &contracts.Request{}
	if err := tx.DB().WithContext(ctx).Where("id = ?", spent.Request).First(row).Error; err != nil {
		// The link was spent and the request it named is gone: the same refusal, because a
		// caller holding an open link cannot act on any finer an answer.
		return nil, contracts.ErrGrant
	}
	if row.Bundle == uuid.Nil {
		return nil, contracts.ErrGrant
	}
	return &contracts.Link{Request: row.ID, Subject: row.Subject, File: row.Bundle}, nil
}

// ExportWalk is the module's periodic work — the thing an outbox cannot express on its
// own, because producing a bundle is a walk rather than a reaction to something that
// happened. Per tenant through jobs.PerTenant, the way modules/task's SLA sweep does it,
// so the worker's RLS bound is the only scope the walk has: the job never names a tenant.
//
// One instance per tick takes the cluster's share of the work (kit/jobs holds an advisory
// lock named after the job), and every request runs in its own transaction, so one
// person's failing bundle does not stop the queue beside it.
func ExportWalk(tenants jobs.TenantLister, svc Walker, every time.Duration) jobs.Job {
	return jobs.Job{
		Name:  "subject-export",
		Every: every,
		Run: func(ctx context.Context, conn *db.Conn) error {
			if tenants == nil {
				// An error, not a panic: a tick that cannot list tenants has nothing to walk,
				// and a worker that died here would look like a broken scheduler rather than a
				// composition that named no lister.
				return errors.New("subject: the export walk has no tenant lister, so it cannot ask which tenants to walk")
			}
			return jobs.PerTenant(ctx, conn, tenants, func(ctx context.Context, conn *db.Conn, _ tenancy.Tenant) error {
				return svc.WalkTenant(ctx, conn)
			})
		},
	}
}

// Walker is the one thing the job needs of the service: one tenant's share of the queue.
// It is a named port rather than a type assertion on the concrete service so the job's
// dependency is the sentence "produce this tenant's bundles" and not a struct a caller
// could not build.
type Walker interface {
	WalkTenant(ctx context.Context, conn *db.Conn) error
}

// WalkTenant is one tenant's tick: claim the open requests, then produce each bundle in a
// transaction of its own, so one person's failing bundle does not stop the queue beside it.
// Every failure comes back joined, each named by its request row — "the walk failed" with no
// row to look at is not something anybody can act on (the same reason as modules/task's
// sweep).
func (s *service) WalkTenant(ctx context.Context, conn *db.Conn) error {
	ids, err := s.claim(ctx, conn)
	if err != nil {
		return err
	}
	var bad []error
	for _, id := range ids {
		if err := s.runOne(ctx, conn, id); err != nil {
			bad = append(bad, fmt.Errorf("subject: export request %s: %w", id, err))
		}
	}
	return errors.Join(bad...)
}

// claim takes the open requests and marks them `running`, in one transaction.
//
// FOR UPDATE SKIP LOCKED is the concurrency control: a tick that overlaps the previous
// one, or a second replica that took the lock while the first is still working, takes
// different rows rather than the same ones twice. The state it writes is what makes the
// row nobody's bundle any more, so a worker that dies mid-bundle leaves a `running` row
// for an operator to see rather than silently re-running forever.
func (s *service) claim(ctx context.Context, conn *db.Conn) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var rows []contracts.Request
		if err := tx.DB().WithContext(ctx).Raw(
			`SELECT * FROM subject_requests WHERE kind = ? AND state = ? ORDER BY created_at, id LIMIT ? FOR UPDATE SKIP LOCKED`,
			contracts.KindExport, contracts.StateRequested, walkLimit).Find(&rows).Error; err != nil {
			return err
		}
		out := make([]uuid.UUID, 0, len(rows))
		for _, r := range rows {
			if err := tx.DB().WithContext(ctx).Model(&contracts.Request{}).
				Where("id = ? AND revision = ?", r.ID, r.Revision).
				UpdateColumns(map[string]any{
					"state": contracts.StateRunning, "revision": r.Revision + 1, "updated_at": s.now(),
				}).Error; err != nil {
				return err
			}
			out = append(out, r.ID)
		}
		ids = out
		return nil
	})
	return ids, err
}

// runOne produces one bundle, stores it, files it and tells the person.
//
// One transaction, so the bundle's file row, the request's new state and the notification
// commit together or not at all: a request that says `ready` about a bundle nobody stored
// is the failure this design cannot afford, and neither is a notice about a bundle that
// was rolled back. A failure of any of it rolls all of it back, and a second transaction —
// which cannot fail the first way — records that this one did.
func (s *service) runOne(ctx context.Context, conn *db.Conn, id uuid.UUID) error {
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row := &contracts.Request{}
		if err := tx.DB().WithContext(ctx).Where("id = ?", id).First(row).Error; err != nil {
			return crud.Classify(err)
		}
		if row.State != contracts.StateRunning || row.Kind != contracts.KindExport {
			return nil
		}
		bundle, err := s.Export(ctx, tx, row.Subject)
		if err != nil {
			return err
		}
		at := s.now()
		expires := at.Add(contracts.LinkLife)
		fileID, err := s.bundles.Put(ctx, func(context.Context) (db.Tx[db.Tenant], error) { return tx, nil },
			fmt.Sprintf("export-%s.zip", row.ID), bundle.Zip)
		if err != nil {
			return err
		}
		// Held from the moment it exists: the walk's own instant is the floor, and every
		// link minted later pushes it out again (contracts.BundleSource).
		if err := s.bundles.Hold(ctx, tx, fileID, expires); err != nil {
			return err
		}
		if err := s.notify(ctx, tx, row, expires); err != nil {
			return err
		}
		if err := tx.DB().WithContext(ctx).Model(&contracts.Request{}).
			Where("id = ? AND revision = ?", row.ID, row.Revision).
			UpdateColumns(map[string]any{
				"state": contracts.StateReady, "bundle_file_id": fileID,
				"bundle_digest": bundle.Digest, "detail": "", "updated_at": at,
				"revision": row.Revision + 1,
			}).Error; err != nil {
			return err
		}
		return events.Publish(ctx, tx, contracts.EventExportReady, contracts.ExportReady{
			Request: row.ID, Subject: row.Subject, Bundle: fileID,
			Digest: bundle.Digest, ExpiresAt: expires, At: at,
		})
	})
	if err == nil {
		return nil
	}
	// The failure gets its own transaction because it has to survive the one that failed:
	// the row goes to `failed` with the reason, which is what makes "the worker said no" a
	// question an operator can answer from the row rather than from the log.
	s.markFailed(ctx, conn, id, err)
	return err
}

// markFailed files the failure. Its own error is worth less than the first one, so the
// first is what the caller is handed and this one is dropped rather than joined: a worker
// that reported two errors would report the less actionable one last.
func (s *service) markFailed(ctx context.Context, conn *db.Conn, id uuid.UUID, cause error) {
	_ = db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().WithContext(ctx).Model(&contracts.Request{}).
			Where("id = ? AND state = ?", id, contracts.StateRunning).
			UpdateColumns(map[string]any{
				"state": contracts.StateFailed, "detail": cause.Error(), "updated_at": s.now(),
			}).Error
	})
}

// notify says an export is ready, to the person who asked. It points at the request and
// never at the token: the in-app notice lives in a row, and a link in a row is a link a
// database read hands out (ADR 0020 §6). The person mints their own door from the request.
func (s *service) notify(ctx context.Context, tx db.Tx[db.Tenant], row *contracts.Request, expires time.Time) error {
	if s.notifier == nil {
		return nil
	}
	return s.notifier.Ready(ctx, tx, contracts.Notice{
		Recipient: row.Requester,
		Title:     "Your export is ready",
		Body: "The copy of your data is ready to download. The link opens once and stays available until " +
			expires.UTC().Format(time.RFC3339) + ".",
		Link: requestPath + row.ID.String(),
	})
}
