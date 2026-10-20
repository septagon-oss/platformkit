// This file is this module's answer to one contract: personaldata.Data (ADR 0020).
//
// What this module holds about a person is two facts no other door records: the
// identifier the identity provider calls them by, and which of the provider's
// collections they were in. Export is a read of those two, in the SCIM shape this
// module's own contracts use; Erase takes the person's own strings out of the
// identifier. Both run inside the caller's transaction, inside the tenant
// row-level security already bounded, so neither names tenant_id.
//
// It is a contributor and never a command: no code in this module calls it. The
// module that composes the parts (modules/subject) does, and the kernel reads the
// same declaration off the manifest to refuse a column this file does not scrub.
package internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/modules/scim/contracts"
)

// personalData is the provisioning door's record of one person. The row stays and
// its key — the person's id — stays with it, as it does in every table this
// application erases about: the audit trail has to keep saying who it acted for.
type personalData struct{}

// PersonalData is this module's contribution to a person's export and erasure.
func PersonalData() personaldata.Part { return personaldata.Part{Module: "scim", Data: personalData{}} }

type scimPart struct {
	// ExternalID is the identifier the provider named this person by, as it was sent.
	ExternalID string `json:"externalId,omitempty"`

	// ProviderDeactivated says this door — and no other door — is the one that
	// stopped the person signing in. It is in the person's copy because it is a
	// fact recorded about them, and it is the only column of the row that is.
	ProviderDeactivated bool `json:"providerDeactivated,omitempty"`

	// Groups are the provider's collections this person belongs to, as the module
	// reads them for its own door.
	Groups []contracts.GroupRef `json:"groups,omitempty"`
}

func (personalData) Export(ctx context.Context, tx db.Tx[db.Tenant],
	s personaldata.Subject) (json.RawMessage, error) {
	var held struct {
		ExternalID  string `gorm:"column:external_id"`
		Deactivated bool   `gorm:"column:provider_deactivated"`
	}
	err := tx.DB().WithContext(ctx).Table("scim_user_external").
		Select("external_id, provider_deactivated").Where("user_id = ?", s.ID).Take(&held).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("scim: export %s's provisioning record: %w", s.ID, err)
	}
	groups, err := groupsOf(tx, s.ID)
	if err != nil {
		return nil, err
	}
	return json.Marshal(scimPart{
		ExternalID: held.ExternalID, ProviderDeactivated: held.Deactivated, Groups: groups,
	})
}

// Erase scrubs external_id across the tenant's rows and not only this person's: the
// column holds whatever string the provisioning client sent, and a directory that
// keys people by mail sends their address, so the identifier of the person asking
// may sit in a row this module thought was somebody else's.
//
// What survives is what every other contributor leaves standing: an identifier the
// provider invented for itself ("00u1a2b3c4"), because a pseudonymous id of the
// person's own employer is the same kind of fact as the uuid this application keeps
// in every other row. A person's own address, name or handle cannot survive here —
// the module declares the column as Text, and the orchestrator re-reads it.
//
// scim_group_members is asked nothing: it carries two uuids and no words, and its
// rows name the person by id, which is what an erasure keeps.
func (personalData) Erase(ctx context.Context, tx db.Tx[db.Tenant], s personaldata.Subject) error {
	_, err := personaldata.Scrub(ctx, tx, s, "scim_user_external", []string{"external_id"}, "")
	return err
}
