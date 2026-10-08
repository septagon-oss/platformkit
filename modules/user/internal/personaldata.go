// This file is this module's answer to one contract: personaldata.Data (ADR 0020).
//
// Export is a read of the rows this module owns about one person, in the shape this
// module's own contracts use. Erase removes what those rows *say* and keeps the rows
// wherever the audit trail has to keep a row; the person's id survives, the strings
// that named them do not. Both run inside the caller's transaction, inside the tenant
// row-level security already bounded, so neither names tenant_id.
//
// It is a contributor and never a command: no code in this module calls it. The module
// that composes the parts (modules/subject) does, and the kernel reads the same
// declaration below off the manifest to refuse a column this file does not scrub.
package internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

// personalData is users, which is the one table where the person is the row itself:
// the id stays, every value that named them is replaced, and nothing is deleted — the
// row the audit trail points at has to go on existing.
//
// It needs the service for one rule it cannot answer alone: erasing the last person
// who can still administer a tenant would take the tenant's own door away (house rule
// 8), and who can administer depends on what roles grant, which is the auth module's
// table. So the composition hands the floor down; the erasure asks it before it writes.
type personalData struct{ users *Service }

// PersonalData is this module's contribution to a person's export and erasure. It
// takes the service for one rule it cannot answer alone — see personalData above.
func PersonalData(users *Service) personaldata.Part {
	return personaldata.Part{Module: "user", Data: personalData{users: users}}
}

// userPart is the bundle's shape. password_hash is absent on purpose: Article 15 gives
// a person their data, and an argon2 hash of a password they may still be using on
// another service is not theirs to be handed back — it is the credential's, and a
// export that carried it would be a copy of a secret in a zip that travels by email.
type userPart struct {
	Users []userRow `json:"users"`
}

type userRow struct {
	ID        uuid.UUID  `json:"id"`
	Email     string     `json:"email"`
	Name      string     `json:"displayName"`
	Handle    *string    `json:"handle,omitempty"`
	Status    string     `json:"status"`
	Roles     string     `json:"roles"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
	DeletedAt *time.Time `json:"deletedAt,omitempty"`
}

func (personalData) Export(ctx context.Context, tx db.Tx[db.Tenant],
	s personaldata.Subject) (json.RawMessage, error) {
	var rows []userRow
	// No deleted_at filter: a soft-deleted person still has a row, and "what do you
	// hold about me" does not stop at a row the directory hides.
	err := tx.DB().WithContext(ctx).Table("users").
		Select("id::text AS id, email, display_name, handle, status, roles::text AS roles, "+
			"created_at, updated_at, deleted_at").
		Where("id = ?", s.ID).Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("user: export %s: %w", s.ID, err)
	}
	return json.Marshal(userPart{Users: rows})
}

// Erase replaces the identity in place. The address becomes a per-tenant unique value
// on a domain RFC 2606 reserved, so users_tenant_email still holds and the real address
// is released for reuse; the handle goes NULL, which is the one value the handle's
// CHECK and its partial index both allow; the password and the roles go, because a
// person with no name in the record has no business holding a door or a grant.
//
// A row that is not there is an answer and not a failure — an erasure of nothing is a
// receipt of zero, so the redelivery of an erasure does not fail the second time.
func (p personalData) Erase(ctx context.Context, tx db.Tx[db.Tenant], s personaldata.Subject) error {
	var before *contracts.User
	err := tx.DB().WithContext(ctx).Raw(
		"SELECT id, tenant_id, created_at, updated_at, deleted_at, email, display_name, status, "+
			"roles, password_hash, handle FROM users WHERE id = ? FOR UPDATE", s.ID).Scan(&before).Error
	if errors.Is(err, crud.ErrNotFound) || (err == nil && before == nil) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("user: lock %s: %w", s.ID, err)
	}
	if err := p.users.RefuseLastAdministrator(ctx, tx, before); err != nil {
		return err
	}
	res := tx.DB().WithContext(ctx).Exec(
		`UPDATE users
		    SET email = ?, display_name = '', handle = NULL, password_hash = NULL,
		        roles = '{}'::text[], updated_at = ?
		  WHERE id = ?`, s.ErasedEmail(), db.Now(), s.ID)
	if res.Error != nil {
		return fmt.Errorf("user: erase %s: %w", s.ID, res.Error)
	}
	return nil
}
