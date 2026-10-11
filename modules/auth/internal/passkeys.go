package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
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

// passkeyRequest is the request this ceremony answers. Every leg of the passkey
// half is an HTTP ceremony: the relying party is built from the host it arrived
// at, and the origin a browser will claim is read off the same request, so a
// ceremony with no request is not a ceremony with a default — it is a call nobody
// can answer.
func (s *Service) passkeyRequest(ctx context.Context) (*http.Request, error) {
	r, ok := httpx.RequestFrom(ctx)
	if !ok || r == nil {
		return nil, errors.New("auth: a passkey ceremony answers an HTTP request")
	}
	return r, nil
}

// mayPrompt is the cap on the public ceremony legs: how many prompts one address
// may have begun in the window (contracts.PasskeyPrompts). It is asked inside the
// command rather than on the route because the write it caps is made here, and a
// command capped by its caller is a command one new route away from uncapped.
func (s *Service) mayPrompt(ctx context.Context, r *http.Request) bool {
	return s.limiter.Prompted(ctx, ClientOf(r).IP)
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

// BeginPasskeyAssertion begins the second half of a sign-in a password earned. It
// learns nothing and writes nothing but the nonce, and it costs the same for every
// address because it does not read the one it was sent: the prompt is the
// discoverable one, an empty allow list on purpose, so the request a browser makes
// for an address that holds ten passkeys is byte-for-byte the request for an
// address that holds none.
//
// The address is therefore not looked up at all. It used to be, and only when the
// answer was "nobody here", to pay the argon2id the login path pays so a stopwatch
// could not tell the two apart — which worked, and cost one indexed read per known
// address to hide the difference the lookup itself created. Spending hash work to
// hide a branch is a way of keeping the branch; not taking it is the smaller and
// stronger answer, and it is the one the route's sentence already claimed.
func (s *Service) BeginPasskeyAssertion(ctx context.Context, tx db.Tx[db.Tenant], email string) (*contracts.PasskeyChallenge, error) {
	r, err := s.passkeyRequest(ctx)
	if err != nil {
		return nil, err
	}
	if !s.mayPrompt(ctx, r) {
		return nil, contracts.ErrTooManyAttempts
	}
	rp, err := s.passkeyRelyingParty(r, tx)
	if err != nil {
		return nil, err
	}
	assertion, session, err := rp.BeginDiscoverableLogin()
	if err != nil {
		return nil, fmt.Errorf("auth: begin a passkey assertion: %w", err)
	}
	return s.storeCeremony(ctx, tx, contracts.PasskeyCeremonySecondFactor, nil, session, assertion)
}

// SetPasskeySignIn writes the tenant's own row at the usernameless door.
//
// The read and the write are one step because they are one transaction, and the
// row's only predicate is the tenant the request already resolved: RLS holds it
// to this tenant's row, so there is no argument here for a caller to point at
// another tenant with, and no tenant id to check a caller was given honestly.
// The authority is the route's own permission check — PermissionPasskeySignIn,
// asked by the kernel in this same transaction — which is how every other
// permission-guarded write in this module is arranged; the ceremony commands
// recheck something narrower (whose ceremony is this) because the route cannot.
//
// Writing the value that is already written is not a change: no row is touched,
// no event is published, and the answer is the state either way. An event for a
// write that wrote nothing would put a door-opening in the trail that nobody
// opened, and modules/audit would copy the lie faithfully.
//
// "Already written" is a fact about a moment, and two requests that arrive
// together disagree about the moment. Read first and write unconditionally, and
// two requests enabling one disabled door both read false, both write true, and
// the trail records the door opening twice for a door that opened once — with
// the setting itself right, which is what makes the wrong history so hard to
// notice. So the read takes the row's lock and the write carries its own guard.
//
// Both halves are needed, and they are needed because they cover different cases.
// FOR UPDATE is what makes the read still true when the write lands: a contender
// waits for it and then re-reads the version the winner committed, so `was` is
// what the trail is about to say it was. The guard —
// `WHERE sign_in IS DISTINCT FROM EXCLUDED.sign_in` — is what a lock cannot be,
// because a tenant with no row yet has no row to lock: two inserts racing for the
// first one settle at the write, where the loser waits on the row the winner
// inserted, re-checks the guard against it, affects no row, and publishes nothing.
// A transition the guard refused is not a change, and the second trail row would
// describe a change nobody made.
func (s *Service) SetPasskeySignIn(ctx context.Context, tx db.Tx[db.Tenant], enabled bool) (bool, error) {
	was, err := s.lockedPasskeySignIn(ctx, tx)
	if err != nil {
		return false, err
	}
	if was == enabled {
		return was, nil
	}
	tenant := db.TenantOf(tx)
	res := tx.DB().Exec(
		"INSERT INTO passkey_settings (tenant_id, sign_in) VALUES (?, ?) "+
			"ON CONFLICT (tenant_id) DO UPDATE SET sign_in = EXCLUDED.sign_in "+
			"WHERE passkey_settings.sign_in IS DISTINCT FROM EXCLUDED.sign_in",
		tenant.ID, enabled)
	if res.Error != nil {
		return false, fmt.Errorf("auth: set the passkey sign-in door of %s: %w", tenant.ID, res.Error)
	}
	if res.RowsAffected == 0 {
		// The guard found the value this request asked for already written, by a
		// transaction that won the row this one had nothing to lock. The answer is
		// the state, and the state did not change under this request's hand.
		return enabled, nil
	}
	if err := events.Publish(ctx, tx, contracts.EventPasskeySignInSet, contracts.PasskeySignInSet{
		Was: was, Now: enabled, At: db.Now(),
	}); err != nil {
		return false, err
	}
	return enabled, nil
}

// BeginPasskeySignIn begins the usernameless ceremony, for a tenant that enabled
// the door. No address is offered and none is learned.
func (s *Service) BeginPasskeySignIn(ctx context.Context, tx db.Tx[db.Tenant]) (*contracts.PasskeyChallenge, error) {
	enabled, err := s.passkeySignInEnabled(ctx, tx)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, contracts.ErrPasskeySignInOff
	}
	r, err := s.passkeyRequest(ctx)
	if err != nil {
		return nil, err
	}
	if !s.mayPrompt(ctx, r) {
		return nil, contracts.ErrTooManyAttempts
	}
	rp, err := s.passkeyRelyingParty(r, tx)
	if err != nil {
		return nil, err
	}
	assertion, session, err := rp.BeginDiscoverableLogin()
	if err != nil {
		return nil, fmt.Errorf("auth: begin a passkey sign-in: %w", err)
	}
	return s.storeCeremony(ctx, tx, contracts.PasskeyCeremonySignIn, nil, session, assertion)
}

// FinishPasskeyAssertion is the answer to either sign-in door, and the order of its
// steps is the whole of its security.
//
// The nonce is spent before anything is read about a factor, so every refusal
// below — a wrong signature, an unknown credential, a person who cannot sign in, a
// clone — costs the attempt. A refusal that left the row alive would leave a
// captured assertion replayable until it expired, which is a sign-in for anybody
// who can POST one body twice, with no biometric and no new ceremony.
//
// The tenant's policy is rechecked below for the door it governs, in the transaction that
// would open the session: an administrator who shut the usernameless door while a prompt
// stood open meant to shut it now, and a command answering from the policy as of the nonce
// would open a session the tenant had just refused.
//
// The first-factor proof is spent after that and before the session, when and only
// when the row says the ceremony was begun at the second-factor door. That is the
// line that keeps a passkey from weakening the pair: a valid signature over a
// challenge this server minted is still not a sign-in for an account that never
// offered its password. Which door it was is the row's fact and never the caller's
// argument, which is what stops a second-factor ceremony being answered at the door
// that expects no password — the refusal for that mix-up is the same 401 at the
// same cost as a bad signature, and it spends nothing.
func (s *Service) FinishPasskeyAssertion(ctx context.Context, tx db.Tx[db.Tenant], ceremony uuid.UUID, response json.RawMessage, from contracts.Client) (*contracts.Session, *contracts.Identity, error) {
	if !s.MayRedeem(ctx, from.IP) {
		return nil, nil, contracts.ErrTooManyAttempts
	}
	r, err := s.passkeyRequest(ctx)
	if err != nil {
		return nil, nil, err
	}
	rp, err := s.passkeyRelyingParty(r, tx)
	if err != nil {
		return nil, nil, err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(response)
	if err != nil {
		return nil, nil, contracts.ErrCredentials
	}
	session, door, spent, err := s.spendCeremony(ctx, tx, ceremony, nil,
		contracts.PasskeyCeremonySignIn, contracts.PasskeyCeremonySecondFactor)
	if err != nil {
		return nil, nil, err
	}
	if !spent {
		return nil, nil, contracts.ErrCredentials
	}
	// The usernameless door, rechecked in this transaction; the second-factor door is not
	// governed by this setting, since a passkey answers a password whether or not one may
	// replace it. 403 and not 401: nothing was wrong with what the person offered, and the
	// refusal is the tenant's own decision, said as BeginPasskeySignIn says it. It comes
	// after the nonce was spent and the attempt charged, before the signature is credited,
	// so it opens no session, spends no first-factor proof and publishes nothing.
	if door == contracts.PasskeyCeremonySignIn {
		enabled, err := s.passkeySignInEnabled(ctx, tx)
		if err != nil {
			return nil, nil, err
		}
		if !enabled {
			return nil, nil, contracts.ErrPasskeySignInOff
		}
	}
	var holds []passkeyCredentialRow
	owner := func(rawID, userHandle []byte) (webauthn.User, error) {
		user, rows, err := s.passkeyOwner(ctx, tx, rawID, userHandle)
		if err != nil {
			return nil, err
		}
		holds = rows
		return passkeyUser{user: user, holds: credentialsOf(rows)}, nil
	}
	user, cred, err := rp.ValidatePasskeyLogin(owner, *session, parsed)
	if err != nil {
		return nil, nil, contracts.ErrCredentials
	}
	who, ok := user.(passkeyUser)
	if !ok || !who.user.CanSignIn() {
		return nil, nil, contracts.ErrCredentials
	}
	// From here the actor this transaction records is the person whose signature it
	// just verified. The request arrived as nobody — both doors admit an anonymous
	// caller, which is the point of them — or, if a cookie came along, as somebody
	// else: an extra session in the jar makes a valid assertion no less the answer
	// this server asked for, and the session it opens belongs to the signer. The
	// pre-request principal would put a sign-in in the trail under the person who
	// happened to be holding a cookie, and the trail's actor filter — the answer to
	// "who signed in as this person" — would find nothing for the person who did.
	// Everything published below is about this signature and nobody else's.
	ctx = tenancy.WithActor(ctx, who.user.ID)
	factor, err := s.recordPasskeyUse(ctx, tx, who.user.ID, cred, holds)
	if err != nil {
		return nil, nil, err
	}
	if door == contracts.PasskeyCeremonySecondFactor {
		if err := s.RequireFirstFactorProof(ctx, tx, who.user.Email, from); err != nil {
			// One line, said to nobody but the operator, for the one refusal at this
			// door that is not about the answer at all. To the caller the two facts stay
			// indistinguishable — same 401, same sentence, same cost — which is what
			// RequireFirstFactorProof's own comment asks for and what this does not
			// change. What it cannot be is silent: the person's device answered a prompt
			// this server minted, and the answer they get says their passkey did not
			// answer. That sentence is about their key, and the thing that failed is our
			// own window, whose write is detached and therefore can come back having
			// written nothing while still refusing to say so (markFirstFactorProved).
			// A browser journey that lost a sign-in this way had nothing in its server's
			// log to say so: every one of six refusals below shares one error value.
			if errors.Is(err, contracts.ErrCredentials) {
				slog.WarnContext(ctx, "auth: a passkey answered and the first-factor window was not there",
					"user", who.user.ID)
			}
			return nil, nil, err
		}
	}
	session_, identity, err := s.open(ctx, tx, who.user, from, "passkey")
	if err != nil {
		return nil, nil, err
	}
	return session_, identity, events.Publish(ctx, tx, contracts.EventFactorUsed, contracts.FactorUsed{
		UserID: who.user.ID, FactorID: factor, Kind: "passkey", Door: door, At: db.Now(),
	})
}

// passkeyOwner answers the SDK's discoverable lookup: whose credential is this, and the
// rows that say what they hold. The credential id is public and spends nothing, so
// reading it before the signature is checked is not a lookup an attacker drives — and
// the RLS policy makes another tenant's row invisible here, which is the database half
// of "a passkey for tenant A never answers at tenant B". The rows travel back to the
// caller rather than being read again, because recordPasskeyUse writes against the
// counter this read handed the library.
//
// The user handle is checked to be this credential's owner's id rather than merely
// present: the library compares it against the user it is given, and the row is the
// only thing that could tie a key to a person.
func (s *Service) passkeyOwner(ctx context.Context, tx db.Tx[db.Tenant], rawID, userHandle []byte) (*usercontracts.User, []passkeyCredentialRow, error) {
	userID, err := uuid.FromBytes(userHandle)
	if err != nil {
		return nil, nil, crud.ErrNotFound
	}
	var match []passkeyCredentialRow
	if err := tx.DB().Where("credential_id = ? AND user_id = ?", rawID, userID).
		Find(&match).Error; err != nil {
		return nil, nil, err
	}
	if len(match) == 0 {
		return nil, nil, crud.ErrNotFound
	}
	user, err := s.users.Get(ctx, tx, userID)
	if err != nil {
		return nil, nil, err
	}
	rows, err := s.passkeyCredentials(ctx, tx, userID)
	if err != nil {
		return nil, nil, err
	}
	return user, rows, nil
}

// recordPasskeyUse is the clone rule and the counter, in that order, with the decision
// inside the statement that makes it. holds is the rows the ceremony's own lookup read:
// the counter this write must not move is the one the signature was validated against,
// and a second read could name a counter another assertion had already advanced — which
// would let this answer *lower* the stored count rather than refuse.
//
// The library's UpdateCounter already set CloneWarning on the credential it handed
// back, applying the standard's own exception that two zero counters are not a
// clone — a credential that reports no counter is legal, and refusing it would lock
// out the first large cohort of real passkeys on day one. What the library cannot
// decide is what to do about one, so this does: the credential is put out of use
// permanently, the trail records the two counters, and the person is refused as
// they are for any wrong answer. "Your passkey was flagged as cloned" is a sentence
// that helps whoever cloned it more than the person it was cloned from.
//
// The write is a compare-and-set, and `sign_count = ?` is the member that makes it one.
// Without it two assertions carrying one counter both validate against one stored value —
// each reads the row before either has written it — and both open a session, the clone rule
// arriving one commit too late. With it the loser waits on the row lock the winner's UPDATE
// holds, Postgres re-checks its predicate against the version that write produced, it
// affects no row, and the person is refused as for a replay. The lookup's read may stay
// unlocked: what it may be stale about is what the predicate re-reads under that lock.
//
// A refused answer writes no verdict — its counter was stale, and the clone rule fires
// on the next answer against the winner's — and what it cannot do is open a session.
//
// It reports the factor's id so the successful use can name what answered.
func (s *Service) recordPasskeyUse(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID,
	cred *webauthn.Credential, holds []passkeyCredentialRow) (uuid.UUID, error) {
	at := slices.IndexFunc(holds, func(row passkeyCredentialRow) bool {
		return bytes.Equal(row.CredentialID, cred.ID)
	})
	if at < 0 {
		// The library answers for a credential out of the list it was handed, so a
		// signature this cannot place is one nobody here was asked to sign.
		return uuid.Nil, contracts.ErrCredentials
	}
	row := holds[at]
	if cred.Authenticator.CloneWarning {
		// The verdict has to outlive the refusal that delivers it. A 401 rolls the
		// request's transaction back — kit/httpx commits only below 400 — so a flag
		// and an event written here would be undone by the very answer that discovered
		// the clone: the credential would still be usable tomorrow and the trail would
		// hold nothing about why anybody thought it was not. The write therefore goes
		// outside the request's transaction, the way spending a nonce does, for the
		// reason stated there and with the same budget.
		//
		// What it cannot do is soften the answer: the person is refused as they are
		// for any wrong signature, and the flag is the module's own record, read by
		// whoever is told about a cloned authenticator.
		if err := s.flagPasskeySuspect(ctx, row.ID, userID, row.SignCount,
			int64(cred.Authenticator.SignCount)); err != nil {
			slog.ErrorContext(ctx, "auth: the suspect-passkey record could not be written",
				"user", userID, "factor", row.ID, "error", err)
		}
		return row.ID, contracts.ErrCredentials
	}
	// The guard is in the UPDATE rather than in a comparison: two tabs answering
	// with one credential settle on one accepted counter, and a credential whose
	// clone warning was set between the read and the write is refused by the
	// predicate rather than by whoever got there first.
	res := tx.DB().Model(&passkeyCredentialRow{}).
		Where("id = ? AND clone_warning = false AND sign_count = ?", row.ID, row.SignCount).
		Update("sign_count", int64(cred.Authenticator.SignCount))
	if res.Error != nil {
		return row.ID, fmt.Errorf("auth: record a passkey use: %w", res.Error)
	}
	if res.RowsAffected != 1 {
		return row.ID, contracts.ErrCredentials
	}
	return row.ID, nil
}

// flagPasskeySuspect writes the clone verdict and the event that describes it,
// committed on its own, before the request that discovered the clone is refused.
//
// The UPDATE carries no counter: a credential flagged as suspect is refused by
// recordPasskeyUse's own predicate from here on, and the two counters the review
// needs are in the event rather than in the row that has already stopped answering.
func (s *Service) flagPasskeySuspect(ctx context.Context, factorID, userID uuid.UUID,
	previous, observed int64) error {
	conn, ok := httpx.ConnFrom(ctx)
	if !ok {
		return errors.New("auth: no connection to record a suspect passkey on")
	}
	detached, cancel := context.WithTimeout(db.Detached(context.WithoutCancel(ctx)), detachedWriteBudget)
	defer cancel()
	return db.Run(detached, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		res := tx.DB().Model(&passkeyCredentialRow{}).Where("id = ?", factorID).
			Update("clone_warning", true)
		if res.Error != nil {
			return fmt.Errorf("auth: record a suspect passkey: %w", res.Error)
		}
		if res.RowsAffected != 1 {
			// The credential was withdrawn between the read and the verdict, so there
			// is no factor left to put out of use and no row an event could name.
			return nil
		}
		// The one log line this delivery writes, and it names two ids: no credential
		// material is ever logged, in an event or a line.
		slog.WarnContext(ctx, "auth: a passkey reported a counter going backwards",
			"user", userID, "factor", factorID)
		return events.Publish(ctx, tx, contracts.EventFactorSuspect, contracts.FactorSuspect{
			UserID: userID, FactorID: factorID, Kind: "passkey",
			PreviousCount: previous, ObservedCount: observed, At: db.Now(),
		})
	})
}

// lockedPasskeySignIn is the setting read of a command: the same answer,
// read under the row's lock and held against any other writer of it until this
// transaction finishes.
//
// The unlocked read below stays where the answer is only asked. A command that
// decides a change from what it read is a different matter — rule 9's recheck has
// to be a recheck rather than a hope — and this is the one passkey command that
// writes a value it read. Locking the two reads would put every ceremony in a
// tenant behind every other one to protect nothing: each of them re-checks what it
// needs at the statement that spends or opens something.
func (s *Service) lockedPasskeySignIn(_ context.Context, tx db.Tx[db.Tenant]) (bool, error) {
	tenant := db.TenantOf(tx)
	var rows []struct {
		SignIn bool
	}
	res := tx.DB().Table("passkey_settings").Select("sign_in").
		Where("tenant_id = ?", tenant.ID).Limit(1).
		Clauses(clause.Locking{Strength: "UPDATE"}).Find(&rows)
	if res.Error != nil {
		return false, fmt.Errorf("auth: lock the passkey settings of %s: %w", tenant.ID, res.Error)
	}
	return len(rows) > 0 && rows[0].SignIn, nil
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

// ceremonyRow is one in-flight ceremony as the table holds it.
type ceremonyRow struct {
	Kind      string
	UserID    *uuid.UUID
	Session   []byte
	ExpiresAt time.Time
}

// storeCeremony writes the nonce the answer must match. session is the library's
// own record of the ceremony — the challenge, the relying-party id it was begun
// under, the origin — and it is stored rather than rebuilt so a response collected
// at another origin, or under another id, fails verification against what was
// actually promised rather than against what this request happens to configure now.
func (s *Service) storeCeremony(ctx context.Context, tx db.Tx[db.Tenant], kind string, userID *uuid.UUID, session *webauthn.SessionData, options any) (*contracts.PasskeyChallenge, error) {
	blob, err := json.Marshal(session)
	if err != nil {
		return nil, fmt.Errorf("auth: encode a passkey ceremony: %w", err)
	}
	body, err := json.Marshal(options)
	if err != nil {
		return nil, fmt.Errorf("auth: encode a passkey ceremony's options: %w", err)
	}
	id, at := uuid.New(), db.Now()
	res := tx.DB().Exec(
		"INSERT INTO passkey_challenges (id, tenant_id, user_id, kind, session, expires_at, created_at)"+
			" VALUES (?, ?, ?, ?, ?::jsonb, ?, ?)",
		id, db.TenantOf(tx).ID, userID, kind, blob, at.Add(contracts.PasskeyChallengeWindow), at)
	if res.Error != nil {
		return nil, fmt.Errorf("auth: begin a passkey ceremony: %w", res.Error)
	}
	return &contracts.PasskeyChallenge{Ceremony: id, Options: body}, nil
}

// spendCeremony deletes the row a ceremony answer is being made against and reports
// whether this request is the one that got it, with the door the row names.
//
// The caller names the doors this answer is good at, and the row's own kind is the
// thing compared: a second-factor prompt is spendable at the second-factor answer
// and nowhere else, and the assertion leg names its two sign-in doors rather than
// "anything" — a register ceremony is not a sign-in waiting to be answered, and a
// list a caller forgot to fill matches nothing rather than everything.
//
// A body aimed at the wrong door consumes nothing — the DELETE asks for the kind
// the row itself reported, and the row survives for the door it was minted at.
//
// The delete is a transaction of its own, which is the one new write outside the
// request's transaction in this delivery, and markFirstFactorProved's reasoning is
// the reason: kit/httpx does not commit a request that answers 401, and 401 is the
// ordinary answer at a public ceremony leg, so a DELETE in the caller's transaction
// would be undone by the refusal it was meant to consume. A nonce is not account
// state — consuming it changes nothing about anybody's factors, writes no event and
// returns no session — so "a refused mutation writes nothing" still holds.
//
// A failure is logged and the answer is unchanged: the person is refused and begins
// again, which costs one tap.
func (s *Service) spendCeremony(ctx context.Context, tx db.Tx[db.Tenant], ceremony uuid.UUID, wantUser *uuid.UUID, wantKinds ...string) (*webauthn.SessionData, string, bool, error) {
	var row ceremonyRow
	err := tx.DB().Raw(
		"SELECT kind, user_id, session, expires_at FROM passkey_challenges WHERE id = ?", ceremony,
	).Scan(&row).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, "", false, fmt.Errorf("auth: read a passkey ceremony: %w", err)
	}
	if !slices.Contains(wantKinds, row.Kind) {
		return nil, "", false, nil
	}
	if wantUser != nil && (row.UserID == nil || *row.UserID != *wantUser) {
		return nil, "", false, nil
	}
	if !row.ExpiresAt.After(db.Now()) {
		return nil, "", false, nil
	}
	if !spendCeremonyRow(ctx, ceremony, row.Kind, wantUser) {
		return nil, "", false, nil
	}
	session := &webauthn.SessionData{}
	if err := json.Unmarshal(row.Session, session); err != nil {
		return nil, "", false, fmt.Errorf("auth: decode a passkey ceremony: %w", err)
	}
	return session, row.Kind, true, nil
}

// spendCeremonyRow is the consuming DELETE, outside the request's transaction.
// It asks for the kind the row itself reported, so the DELETE is a compare-and-
// delete against the row this request read rather than against an argument.
func spendCeremonyRow(ctx context.Context, ceremony uuid.UUID, kind string, wantUser *uuid.UUID) bool {
	conn, ok := httpx.ConnFrom(ctx)
	if !ok {
		return false
	}
	detached, cancel := context.WithTimeout(db.Detached(context.WithoutCancel(ctx)), detachedWriteBudget)
	defer cancel()
	var spent int64
	err := db.Run(detached, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		q := tx.DB().Exec(
			"DELETE FROM passkey_challenges WHERE id = ? AND kind = ? AND user_id IS NOT DISTINCT FROM ?"+
				" AND expires_at > now()",
			ceremony, kind, wantUser)
		spent = q.RowsAffected
		return q.Error
	})
	if err != nil {
		slog.ErrorContext(ctx, "auth: could not spend a passkey ceremony", "ceremony", ceremony, "error", err)
		return false
	}
	return spent == 1
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
