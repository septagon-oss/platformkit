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

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"gorm.io/gorm/clause"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

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
	if d := s.MayRedeem(ctx, from.IP); d.Decided() && !d.Allowed {
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
