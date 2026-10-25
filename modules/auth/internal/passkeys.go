package internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"gorm.io/gorm/clause"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// The passkey half of the second factor: the ceremony where this server mints a
// nonce and receives a signature over it, instead of minting a secret and
// receiving a code derived from it.
//
// This is the only file in the repository that imports the WebAuthn SDK, the way
// oidc.go is the only one that imports go-oidc: contracts/passkeys.go names no SDK
// type, so CBOR, COSE and the protocol structs never reach a handler, an event or
// the contract. Two things are decided here that the library deliberately leaves
// to the relying party, and both are stated where they are enforced.
//
// The first is the relying party itself: it is built per request, from the host
// this request arrived at, because that host is what already chose the tenant
// (httpx.withHostTenant resolves HostOnly(r.Host)) — so a passkey minted at one
// tenant's host cannot answer at another's by construction, and there is no
// package-level WebAuthn value holding one tenant's id for everybody (pillar 2).
// The library also writes into the Config it is handed — validate() fills the
// default timeouts and latches `validated` through the pointer — so a cached one
// would be a data race between two concurrent ceremonies, which is a second and
// less charitable reason for the same line.
//
// The second is the clone rule. The library's Authenticator.UpdateCounter sets a
// flag rather than returning an error, and it carries the exception the standard
// asks for: both counters zero is not a clone, because an authenticator that
// reports no counter at all is legal and most phones are one. The flag is read
// here, the columns of 000035 record it, and the refusal — this credential never
// signs anybody in again, and the trail says why — is this module's decision.

// passkeyCredentialRow is one enrolled passkey. Nothing in it is sealed, because
// nothing in it is secret: the credential id identifies a public key and the
// public key is public. Compare totpRow, every row of which needs auth.factor_key.
type passkeyCredentialRow struct {
	ID           uuid.UUID `gorm:"primaryKey"`
	TenantID     uuid.UUID
	UserID       uuid.UUID
	CredentialID []byte
	PublicKey    []byte
	// BackupEligible is the one flag the standard says cannot change and the
	// library checks on every assertion; see the migration for why it and no
	// other flag is kept.
	BackupEligible bool
	SignCount      int64
	CloneWarning   bool
	Name           string
	CreatedAt      time.Time
}

func (passkeyCredentialRow) TableName() string { return "passkey_credentials" }

var _ contracts.Passkeys = (*Service)(nil)

// passkeyUser is one person and their passkeys, in the shape the SDK asks for.
// The user handle is the person's id — the same 16 bytes the row's primary key
// holds — so the handle baked into a discoverable credential is the account, and
// no second identifier for the same person is written anywhere (rule 7).
type passkeyUser struct {
	user  *usercontracts.User
	holds []webauthn.Credential
}

func (u passkeyUser) WebAuthnID() []byte {
	id := u.user.ID
	return id[:]
}

func (u passkeyUser) WebAuthnName() string { return u.user.Email }

func (u passkeyUser) WebAuthnDisplayName() string {
	if u.user.DisplayName != "" {
		return u.user.DisplayName
	}
	return u.user.Email
}

func (u passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.holds }

// credential rebuilds the SDK's view of this row, which is what verification
// needs: the id, the public key, the flag the standard says cannot change, and
// the counter as the last accepted answer reported it.
func (r passkeyCredentialRow) credential() webauthn.Credential {
	var count uint32
	if r.SignCount > 0 {
		count = uint32(min(r.SignCount, int64(^uint32(0))))
	}
	return webauthn.Credential{
		ID:        r.CredentialID,
		PublicKey: r.PublicKey,
		Flags:     webauthn.NewCredentialFlags(backupEligibleFlag(r.BackupEligible)),
		Authenticator: webauthn.Authenticator{
			SignCount:    count,
			CloneWarning: r.CloneWarning,
		},
	}
}

// backupEligibleFlag puts the one stored flag back into the flag word the library
// compares the next assertion against.
func backupEligibleFlag(eligible bool) protocol.AuthenticatorFlags {
	if eligible {
		return protocol.FlagBackupEligible
	}
	return 0
}

func credentialsOf(rows []passkeyCredentialRow) []webauthn.Credential {
	out := make([]webauthn.Credential, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.credential())
	}
	return out
}

// passkeyRelyingParty builds the relying party for one request. See the header.
//
// The name it hands the platform is read from the tenant this request resolved to,
// beside the host that resolved it, so neither is a value one tenant could leave
// baked into a process.
func (s *Service) passkeyRelyingParty(r *http.Request, tx db.Tx[db.Tenant]) (*webauthn.WebAuthn, error) {
	rpID := httpx.HostOnly(r.Host)
	if rpID == "" {
		return nil, errors.New("auth: a passkey ceremony needs the host it was asked at")
	}
	// The display name is the tenant's own — the string a platform shows somebody
	// deciding whether to trust a prompt — and the host this request arrived at is
	// what stands in for a tenant row that carries no name. No constant in this
	// module names one, and no installation-wide string stands in for a customer's
	// name (rule 4).
	name := db.TenantOf(tx).Name
	if name == "" {
		name = rpID
	}
	rp, err := webauthn.New(&webauthn.Config{
		RPID:          rpID,
		RPDisplayName: name,
		RPOrigins:     []string{requestOrigin(r)},
		// What the browser is promised is what the row keeps: the library's own
		// default is five minutes, and a platform still holding a prompt the
		// server has already swept is a refusal nobody can act on.
		Timeouts: webauthn.TimeoutsConfig{
			Registration: webauthn.TimeoutConfig{Timeout: contracts.PasskeyChallengeWindow},
			Login:        webauthn.TimeoutConfig{Timeout: contracts.PasskeyChallengeWindow},
		},
		// Enrolment asks for a discoverable credential — a passkey, not a
		// security-key-shaped factor bound to an allow list — and for the
		// platform's own user verification. Attestation is declined: this module
		// holds no manufacturer registry to check one against (rule 4), so a
		// statement about who made an authenticator is a specialist fact.
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			RequireResidentKey: protocol.ResidentKeyRequired(),
			UserVerification:   protocol.VerificationRequired,
		},
		AttestationPreference: protocol.PreferNoAttestation,
	})
	if err != nil {
		return nil, fmt.Errorf("auth: build the relying party for %s: %w", rpID, err)
	}
	return rp, nil
}

// requestOrigin is the origin the browser collected the ceremony at, port kept:
// the standard strips the port from the RP ID and not from the origin, so an
// installation served on a non-standard port has both. The scheme follows the one
// rule that decides the session cookie's Secure flag, stated once here rather than
// held twice.
func requestOrigin(r *http.Request) string {
	scheme := "https"
	if r.TLS == nil && config.Local(r.Host) {
		scheme = "http"
	}
	return scheme + "://" + r.Host
}

// BeginPasskeyRegistration asks this person's device for a passkey. Beyond the
// nonce it writes nothing: the credential exists only when FinishPasskeyRegistration
// has seen a signature nobody else could have produced, so a tab closed mid-enrolment
// leaves no factor, publishes no event and changes nothing about how this person
// signs in. No factor key is involved — a public key needs no envelope — so this
// never answers ErrNoFactorKey.
func (s *Service) BeginPasskeyRegistration(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID) (*contracts.PasskeyChallenge, error) {
	user, err := s.users.Get(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	if !user.CanSignIn() {
		return nil, contracts.ErrCredentials
	}
	r, err := s.passkeyRequest(ctx)
	if err != nil {
		return nil, err
	}
	holds, err := s.passkeyCredentials(ctx, tx, user.ID)
	if err != nil {
		return nil, err
	}
	rp, err := s.passkeyRelyingParty(r, tx)
	if err != nil {
		return nil, err
	}
	creation, session, err := rp.BeginRegistration(passkeyUser{user: user, holds: credentialsOf(holds)})
	if err != nil {
		return nil, fmt.Errorf("auth: begin a passkey enrolment: %w", err)
	}
	return s.storeCeremony(ctx, tx, contracts.PasskeyCeremonyRegister, &user.ID, session, creation)
}

// FinishPasskeyRegistration enrols what the ceremony answered, bound to the person
// the ceremony was begun for — which is the caller, rechecked here rather than
// assumed from the credential that got them to the route (rule 9).
func (s *Service) FinishPasskeyRegistration(ctx context.Context, tx db.Tx[db.Tenant], userID, ceremony uuid.UUID, response json.RawMessage, name string) (*contracts.Factor, error) {
	r, err := s.passkeyRequest(ctx)
	if err != nil {
		return nil, err
	}
	rp, err := s.passkeyRelyingParty(r, tx)
	if err != nil {
		return nil, err
	}
	name = trimFactorName(name)
	// The nonce first, in a transaction of its own: an enrolment refused after it
	// was answered must not leave an answer that stays answerable.
	session, _, spent, err := s.spendCeremony(ctx, tx, ceremony, &userID, contracts.PasskeyCeremonyRegister)
	if err != nil {
		return nil, err
	}
	if !spent {
		return nil, contracts.ErrPasskeyExpired
	}
	user, err := s.users.Get(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	if !user.CanSignIn() {
		return nil, contracts.ErrCredentials
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(response)
	if err != nil {
		return nil, contracts.ErrCredentials
	}
	holds, err := s.passkeyCredentials(ctx, tx, user.ID)
	if err != nil {
		return nil, err
	}
	cred, err := rp.CreateCredential(passkeyUser{user: user, holds: credentialsOf(holds)}, *session, parsed)
	if err != nil {
		return nil, contracts.ErrCredentials
	}
	at := db.Now()
	row := passkeyCredentialRow{
		ID: uuid.New(), TenantID: db.TenantOf(tx).ID, UserID: user.ID,
		CredentialID: cred.ID, PublicKey: cred.PublicKey,
		BackupEligible: cred.Flags.BackupEligible,
		SignCount:      int64(cred.Authenticator.SignCount),
		Name:           name, CreatedAt: at,
	}
	// One authenticator is one person's factor: the conflict target is the
	// tenant-scoped unique index, and DO NOTHING rather than DO UPDATE makes a
	// race between two tabs one row and one refusal rather than whichever tab
	// wrote last.
	res := tx.DB().Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "tenant_id"}, {Name: "credential_id"}},
		DoNothing: true,
	}).Create(&row)
	if res.Error != nil {
		return nil, fmt.Errorf("auth: enrol a passkey: %w", res.Error)
	}
	if res.RowsAffected != 1 {
		return nil, contracts.ErrPasskeyExists
	}
	if err := events.Publish(ctx, tx, contracts.EventFactorEnrolled, contracts.FactorEnrolled{
		UserID: user.ID, FactorID: row.ID, Kind: "passkey", At: at,
	}); err != nil {
		return nil, err
	}
	return &contracts.Factor{ID: row.ID, Kind: "passkey", Name: name, EnrolledAt: at}, nil
}

// NewPasskeyDoor returns the reader of a tenant's usernameless door.
//
// It is a Service with nothing in it, and that is sound rather than sloppy: the
// one method it answers reads a single row of the tenant the transaction already
// resolved, and touches no user lookup, no notice and no counter — which is
// everything a built Service holds. It is deliberately not NewService with nils
// handed to it, because that constructor also installs the shared rate counters,
// and a door that consulted them would be a door with a limit on it.
func NewPasskeyDoor() contracts.PasskeyDoor { return &Service{} }

var _ contracts.PasskeyDoor = NewPasskeyDoor()

// PasskeySignInEnabled is contracts.PasskeyDoor: the same read the two ceremony
// doors make of their own row, offered to a caller that is describing how this
// tenant signs in rather than signing in.
//
// It is exported as a method and reached through a constructor of its own
// (modules/auth.NewPasskeyDoor) for the reason site.NewLockedReader gives: adding
// it to contracts.Passkeys or contracts.Auth would be a method added to an
// exported interface, which the exported-API gate refuses as a break to anything
// outside this repository that implements them.
func (s *Service) PasskeySignInEnabled(ctx context.Context, tx db.Tx[db.Tenant]) (bool, error) {
	return s.passkeySignInEnabled(ctx, tx)
}

// passkeySignInEnabled reads the tenant this request resolved to: may a passkey
// be the whole sign-in here? No row is "off", because a row exists only because
// somebody turned the door on, so a tenant that never asked for usernameless
// sign-in carries nothing and is refused with the reason rather than with
// ErrCredentials.
//
// The read is inside the transaction the host already resolved the tenant in and
// is never cached on the process — one tenant's answer becoming another tenant's
// is the failure this whole module spends its per-request relying party avoiding
// — and RLS restricts it to this tenant's row even with the predicate misspelled.
func (s *Service) passkeySignInEnabled(_ context.Context, tx db.Tx[db.Tenant]) (bool, error) {
	tenant := db.TenantOf(tx)
	var rows []struct {
		SignIn bool
	}
	res := tx.DB().Table("passkey_settings").Select("sign_in").
		Where("tenant_id = ?", tenant.ID).Limit(1).Find(&rows)
	if res.Error != nil {
		return false, fmt.Errorf("auth: read the passkey settings of %s: %w", tenant.ID, res.Error)
	}
	return len(rows) > 0 && rows[0].SignIn, nil
}

// passkeyCredentials is what one person holds, in the order the list shows it.
func (s *Service) passkeyCredentials(_ context.Context, tx db.Tx[db.Tenant], userID uuid.UUID) ([]passkeyCredentialRow, error) {
	var rows []passkeyCredentialRow
	if err := tx.DB().Where("user_id = ?", userID).Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("auth: read the passkeys of %s: %w", userID, err)
	}
	return rows, nil
}

// maxFactorName is the name budget the list and the removal dialog can show, and
// the same 40 the passkey_credentials CHECK spells as char_length and the route
// schema spells as maxLength. Three places, one number, and a case that enrols a
// name of exactly this many characters is what keeps them one number.
const maxFactorName = 40

// trimFactorName is the whole of the name policy at the module: the schema also
// refuses an untrimmed or control-character name, and a human-readable 40 characters
// is what the list and the removal dialog can show. Refusing an over-long name is
// huma's maxLength; a name that merely needs trimming is trimmed.
//
// The cut is at the 40th character, which in UTF-8 is not the 40th byte. "日本" is
// two characters and six bytes; a name typed in a script whose characters are wider
// than one is a name a person is entitled to give their own key, and slicing it at
// a byte boundary can land in the middle of one. Postgres calls the result invalid
// UTF-8 and refuses the INSERT — after the ceremony nonce was already spent, so the
// person's tap is gone and the name they typed is what says nothing enrolled.
func trimFactorName(name string) string {
	name = strings.TrimSpace(name)
	runes := []rune(name)
	if len(runes) <= maxFactorName {
		return name
	}
	return string(runes[:maxFactorName])
}
