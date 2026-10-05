package internal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm/clause"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// The second factor. contracts.Factors says what each command decides; this file
// decides it, over the two tables of 000031_auth_factors.up.sql.
//
// Two things shape every command below. The first is that a factor secret is
// sealed at rest and only ever opened inside the transaction that is about to
// spend a code — no copy of the table, and no log of a request, carries a
// working factor. The second is that a code is a nonce: RFC 6238's step is the
// thing that must not repeat, so the factor row itself remembers the highest
// step it accepted, and the spend is one guarded UPDATE rather than a lookup
// and a write.

// totpRow is one enrolled TOTP factor. last_step is the replay guard; see the
// migration for why it is a column on the factor and not a table of challenges.
type totpRow struct {
	ID        uuid.UUID `gorm:"primaryKey"`
	TenantID  uuid.UUID
	UserID    uuid.UUID
	Secret    []byte
	CreatedAt time.Time
	LastStep  int64
}

func (totpRow) TableName() string { return "totp_factors" }

// recoveryCodeRow is one printed code. The row keeps its own hash, and used_at
// is the whole of its state: an UPDATE that finds used_at IS NULL either spends
// it or finds it already spent, which is the same one-shot property
// verification_tokens reached for the same reason.
type recoveryCodeRow struct {
	ID        uuid.UUID `gorm:"primaryKey"`
	TenantID  uuid.UUID
	UserID    uuid.UUID
	CodeHash  []byte
	CreatedAt time.Time
	UsedAt    *time.Time
}

func (recoveryCodeRow) TableName() string { return "recovery_codes" }

// firstFactorProofRow is one account whose first factor checked out and whose
// second half has not been answered yet. One row per person, no secret in it,
// and no proved_at beside the expiry: see the migration for who reads what.
type firstFactorProofRow struct {
	UserID    uuid.UUID `gorm:"primaryKey"`
	TenantID  uuid.UUID
	ExpiresAt time.Time
}

func (firstFactorProofRow) TableName() string { return "first_factor_proofs" }

var _ contracts.Factors = (*Service)(nil)

// EnableFactors hands the service the key that seals and opens a factor secret.
// module.go calls it when the deployment set auth.factor_key; a service that was
// never handed one answers ErrNoFactorKey and writes nothing, which is what
// leaves a deployment without a key behaving exactly as it did before this
// capability existed rather than half-enabling it.
func (s *Service) EnableFactors(key []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.factorKey = slices.Clone(key)
}

func (s *Service) factorKeyBytes() []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.factorKey
}

// BeginTOTP mints a secret and reports it. Nothing is written.
//
// The account label is the caller's own address, read back from the user module
// rather than taken from the request, so the label on an authenticator's prompt
// is the address this account is signed in as and not something the caller typed.
func (s *Service) BeginTOTP(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID) (*contracts.TOTPEnrolment, error) {
	if len(s.factorKeyBytes()) == 0 {
		return nil, contracts.ErrNoFactorKey
	}
	secret, err := newTOTPSecret()
	if err != nil {
		return nil, err
	}
	user, err := s.users.Get(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	return &contracts.TOTPEnrolment{
		Secret: secret, Account: user.Email, GeneratedAt: db.Now(),
	}, nil
}

// FinishTOTP is the moment the factor exists, and it exists only because a code
// the person's own device produced checked out against the secret begun above.
//
// The secret comes back in this request, from the form the enrolment page filled
// in, rather than from anywhere the server kept it: an enrolment that sat in a
// pending row would be per-process state the pool could not see (pillar 2), and
// a person who closes the tab begins again rather than continuing. The secret is
// stored sealed; the plaintext exists in this request and in the authenticator.
func (s *Service) FinishTOTP(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID, secret, code string) (*contracts.Factor, []string, error) {
	key := s.factorKeyBytes()
	if len(key) == 0 {
		return nil, nil, contracts.ErrNoFactorKey
	}
	raw, err := decodeTOTPSecret(secret)
	if err != nil {
		return nil, nil, contracts.ErrCredentials
	}
	if _, ok := totpMatches(raw, code, db.Now(), 0); !ok {
		return nil, nil, contracts.ErrCredentials
	}
	// The raw seed goes into the envelope rather than the base32 text it arrived
	// as: what openSecret hands back must be the key HMAC wants, and storing a
	// second encoding of the same secret would be one more thing to get wrong.
	sealed, err := seal(key, raw)
	if err != nil {
		return nil, nil, err
	}
	at := db.Now()
	factor := totpRow{ID: uuid.New(), TenantID: db.TenantOf(tx).ID, UserID: userID,
		Secret: sealed, CreatedAt: at}
	if err := tx.DB().Create(&factor).Error; err != nil {
		return nil, nil, fmt.Errorf("auth: enrol a factor: %w", err)
	}
	codes, err := s.issueRecoveryCodes(ctx, tx, userID)
	if err != nil {
		return nil, nil, err
	}
	if err := events.Publish(ctx, tx, contracts.EventFactorEnrolled, contracts.FactorEnrolled{
		UserID: userID, FactorID: factor.ID, Kind: "totp", At: at,
	}); err != nil {
		return nil, nil, err
	}
	return &contracts.Factor{ID: factor.ID, Kind: "totp", EnrolledAt: at}, codes, nil
}

// ListFactors is what this person holds, both kinds in one list ordered by when
// they were enrolled. A passkey carries the name its owner gave it; a TOTP row has
// no name column, so its Factor.Name is empty rather than invented (rule 7).
func (s *Service) ListFactors(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID) ([]*contracts.Factor, error) {
	var rows []totpRow
	if err := tx.DB().Where("user_id = ?", userID).Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("auth: list the factors of %s: %w", userID, err)
	}
	out := make([]*contracts.Factor, 0, len(rows))
	for _, row := range rows {
		out = append(out, &contracts.Factor{ID: row.ID, Kind: "totp", EnrolledAt: row.CreatedAt})
	}
	keys, err := s.passkeyCredentials(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	for _, row := range keys {
		out = append(out, &contracts.Factor{ID: row.ID, Kind: "passkey", Name: row.Name, EnrolledAt: row.CreatedAt})
	}
	return out, nil
}

// WithdrawFactor ends one factor of either kind, and refuses to end the last.
//
// "The last" means the last that could still answer, not the last row. A credential
// retired as suspect answers nothing for the rest of its life — recordPasskeyUse
// refuses it on every prompt — so it is the record of a factor that was, and not a
// way in. Counting rows would let one of those stand in for a usable factor: the
// account would "hold two" while one of them could never sign anybody in, and the
// withdrawal that left a person holding only that row would be allowed. That is the
// lockout rule 8 refuses, waved through by a number that looked right. So the count
// is of the factors beside this one that could still answer, and nothing is taken
// away unless this one could too: removing a retired credential is allowed however
// little else is here, and removing the last usable one is refused whatever else the
// tables hold. A successful withdrawal therefore never leaves a person who could
// answer a second factor unable to — and the two-tabs case still settles on one
// success, because the loser's locked scan re-reads the rows the winner deleted.
//
// The count and the delete are one decision under one lock: SELECT ... FOR UPDATE
// over this person's factor rows takes every row a concurrent withdrawal could be
// deciding about, so two tabs each holding a different factor cannot both see
// "there are two" and leave the account with none. That is rule 8 enforced by the
// database rather than by whoever wrote this function last.
//
// Since passkeys landed there are two tables to lock, and the order is a rule
// rather than an accident: totp_factors first, passkey_credentials second,
// everywhere. Two locks taken in an order nobody wrote down invert as soon as a
// second call site appears, and two tabs then deadlock rather than race.
func (s *Service) WithdrawFactor(ctx context.Context, tx db.Tx[db.Tenant], userID, factor uuid.UUID) error {
	var held []totpRow
	err := tx.DB().Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ?", userID).Find(&held).Error
	if err != nil {
		return fmt.Errorf("auth: read the factors of %s: %w", userID, err)
	}
	var keys []passkeyCredentialRow
	err = tx.DB().Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ?", userID).Find(&keys).Error
	if err != nil {
		return fmt.Errorf("auth: read the passkeys of %s: %w", userID, err)
	}
	kind, found := "totp", slices.ContainsFunc(held, func(row totpRow) bool { return row.ID == factor })
	if !found {
		kind, found = "passkey", slices.ContainsFunc(keys, func(row passkeyCredentialRow) bool { return row.ID == factor })
	}
	if !found {
		return crud.ErrNotFound
	}
	usable, answers := 0, kind == "totp" // a TOTP row answers as long as it is here
	for _, row := range held {
		if row.ID != factor {
			usable++
		}
	}
	for _, row := range keys {
		if row.CloneWarning {
			continue
		}
		if row.ID == factor {
			answers = true
		} else {
			usable++
		}
	}
	if answers && usable == 0 {
		return contracts.ErrLastFactor
	}
	target := any(&totpRow{})
	if kind == "passkey" {
		target = &passkeyCredentialRow{}
	}
	res := tx.DB().Where("id = ?", factor).Delete(target)
	if res.Error != nil {
		return fmt.Errorf("auth: withdraw factor %s: %w", factor, res.Error)
	}
	if res.RowsAffected != 1 {
		return crud.ErrNotFound
	}
	return events.Publish(ctx, tx, contracts.EventFactorWithdrawn, contracts.FactorWithdrawn{
		UserID: userID, FactorID: factor, Kind: kind, Remaining: len(held) + len(keys) - 1, At: db.Now(),
	})
}

// RotateRecoveryCodes spends every unused code and issues a fresh set, for the
// person who has reason to think the set leaked.
//
// It refuses a person with no factor: codes are the substitute for a second
// factor and not a second one, and a database of codes with no factor beside
// them is a database of second passwords.
func (s *Service) RotateRecoveryCodes(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID) ([]string, error) {
	var held int64
	if err := tx.DB().Model(&totpRow{}).Where("user_id = ?", userID).Count(&held).Error; err != nil {
		return nil, fmt.Errorf("auth: count the factors of %s: %w", userID, err)
	}
	var keys int64
	if err := tx.DB().Model(&passkeyCredentialRow{}).Where("user_id = ?", userID).Count(&keys).Error; err != nil {
		return nil, fmt.Errorf("auth: count the passkeys of %s: %w", userID, err)
	}
	if held+keys == 0 {
		return nil, contracts.ErrNoFactor
	}
	if err := tx.DB().Model(&recoveryCodeRow{}).
		Where("user_id = ? AND used_at IS NULL", userID).
		Update("used_at", db.Now()).Error; err != nil {
		return nil, fmt.Errorf("auth: retire the recovery codes of %s: %w", userID, err)
	}
	codes, err := s.issueRecoveryCodes(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	return codes, nil
}

// VerifySecondFactor is the second half of a sign-in that Login refused to
// finish. It answers with the same session Login would have opened, which is the
// point: a person who proves both things is signed in, and nothing about their
// session says they arrived the long way round except how it was opened.
//
// The half before it is spent by RequireFirstFactorProof, which the challenge
// route asks first, in this same transaction: what is spent there is the fact
// that a door refused this address a moment ago, and without that fact no code
// this method would accept is anybody's sign-in. See the interface note.
//
// An unknown address, a person with no factor, a wrong code, a spent step and a
// spent recovery code are ErrCredentials at one cost, including the equal work
// Login pays for an address nobody has. Whether a wrong answer came from a wrong
// code or from an address with no factor is not something a stranger gets to
// measure, and both publish what a failed login publishes so the trail holds the
// attempt either way.
func (s *Service) VerifySecondFactor(ctx context.Context, tx db.Tx[db.Tenant], email, code string, from contracts.Client) (*contracts.Session, *contracts.Identity, error) {
	// One cap per address on this route, borrowed from the redemption counter:
	// a six-digit code across three steps is 3×10^6 guesses per window, which
	// is hopeless for a person and nothing for a script, so the answer needs a
	// rate limit rather than only a wide space. It is Redeemed and not a new
	// counter because it is the same decision — how often may one address spend
	// a credential this module mailed or minted — and a second counter that
	// means the same thing as the first is two knobs for one policy.
	if !s.MayRedeem(ctx, from.IP) {
		return nil, nil, contracts.ErrTooManyAttempts
	}
	user, err := s.users.ByEmail(ctx, tx, email)
	switch {
	case errors.Is(err, crud.ErrNotFound):
		usercontracts.EqualWork(code)
		return nil, nil, s.fail(ctx, email, from)
	case err != nil:
		return nil, nil, err
	case !user.CanSignIn():
		usercontracts.EqualWork(code)
		return nil, nil, s.fail(ctx, email, from)
	}
	method, err := s.spendFactor(ctx, tx, user.ID, code, db.Now())
	if err != nil {
		if errors.Is(err, contracts.ErrCredentials) {
			return nil, nil, s.fail(ctx, email, from)
		}
		return nil, nil, err
	}
	session, identity, err := s.open(ctx, tx, user, from, method)
	return session, identity, err
}

// RequireFirstFactorProof spends the window one of this address's refused
// sign-ins opened, so that a code is spent by a caller who was asked for one
// rather than by whoever turns up holding one.
//
// The refusal is the same ErrCredentials a wrong code costs, because the two
// answers must not be distinguishable: "sign in with your password first" and
// "that code is wrong" would tell a stranger which half of a sign-in they were
// missing, and the module's discipline at this door is that a stranger learns
// nothing it can act on. It is not 403 and it is not a new status: the caller is
// not signed in and everything else a 401 says is true.
//
// Which door refused is the account's answer, not the caller's: Login and Open
// mint the window when they refuse for the second half (markFirstFactorProved),
// and nothing else writes it. A caller who never offered the first proof has no
// row, so this refuses before a factor is read, before a recovery code is
// looked at, and before anything is spent — the three things a refusal is not
// allowed to do, and what an answer that reaches them would cost the account
// whose codes were in the drawer.
//
// The consumption is a DELETE rather than a flag, for the password_tokens
// reason: the row being gone is what "once" means, and two concurrent answers
// cannot both read an unset flag. The delete is in the caller's transaction, so
// an answer refused *after* it — a wrong code, a replayed step — rolls it back
// along with the rest of a response that kit/httpx will not commit, and the
// person who mistyped one digit has not also lost the window the refusal gave
// them. It costs the account a row lock for the length of one sign-in, which is
// what the same person's own concurrent tabs of one half-finished sign-in are
// worth: two of them answering one code is one session and one refusal, and this
// is the statement that makes it so.
func (s *Service) RequireFirstFactorProof(ctx context.Context, tx db.Tx[db.Tenant], email string, from contracts.Client) error {
	// The lockout Login enforces is enforced here too, and by the same counter:
	// every refusal below records a failure against this account, so a script at
	// the challenge leg is the same account under attack as a script at /login,
	// and it reaches the same ten-in-a-quarter-hour before it is refused. A
	// second counter for this door would be two knobs for one policy and a
	// stranger who only has to switch doors.
	if s.limiter.Check(ctx, email, from.IP) == contracts.Refuse {
		if s.limiter.Noted(ctx, email, from.IP) {
			s.recordFailure(ctx, email, from, true)
		}
		return contracts.ErrTooManyAttempts
	}
	user, err := s.users.ByEmail(ctx, tx, email)
	switch {
	case errors.Is(err, crud.ErrNotFound):
		// The same argon2id an unknown address pays at /login and at the code
		// check, so the three doors cannot be told apart by a stopwatch.
		usercontracts.EqualWork(email)
		return s.fail(ctx, email, from)
	case err != nil:
		return err
	}
	spent := tx.DB().Exec("DELETE FROM first_factor_proofs WHERE user_id = ? AND expires_at > now()", user.ID)
	if spent.Error != nil {
		return fmt.Errorf("auth: spend the first-factor proof of %s: %w", user.ID, spent.Error)
	}
	if spent.RowsAffected != 1 {
		return s.fail(ctx, email, from)
	}
	return nil
}

// markFirstFactorProved records that this account's first factor checked out,
// in a transaction of its own.
//
// Its own, because the response that earns it is a 401 and kit/httpx rolls the
// request's transaction back at 400 and above: written in the caller's
// transaction, the row would be undone by the very refusal that wrote it, which
// is Service.forget's discovery with the sign reversed, and the reason both
// detached writes already in this file exist.
//
// It is a marker and not a credential, and the difference is the point. No
// token, no cookie, nothing handed to the caller that they could hand back: the
// row records a fact the server established by checking a secret, and the only
// statement that can read it is the one that decides whether a code is spendable
// (rule 7). A copy of this table is a list of people who are about to type a
// code and nothing else, which is why no hash sits in it next to code_hash and
// token_hash.
//
// Refreshing rather than adding keeps one live window per person, so the person
// refused four times has one window ending five minutes after the last refusal,
// not four — and it bounds what a scripted attack against this table can grow:
// one row per account, always. It publishes no event. auth.login_failed is the
// trail's record of an attempt that did not get in with what it had, and this
// attempt did get in with the half it had; what it is waiting for is not a sign-in
// and is recorded as no event at all.
//
// A failure is logged and changes the answer not at all: the person is still
// refused until they offer the first factor again, and what they lose is one
// tries' worth of typing. A marker that could not be *refused* would be the
// account.
func (s *Service) markFirstFactorProved(ctx context.Context, userID uuid.UUID) {
	conn, ok := httpx.ConnFrom(ctx)
	if !ok {
		return
	}
	detached, cancel := context.WithTimeout(db.Detached(context.WithoutCancel(ctx)), detachedWriteBudget)
	defer cancel()
	at := db.Now()
	err := db.Run(detached, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"expires_at"}),
		}).Create(&firstFactorProofRow{
			UserID: userID, TenantID: db.TenantOf(tx).ID,
			ExpiresAt: at.Add(contracts.FirstFactorProofWindow),
		}).Error
	})
	if err != nil {
		slog.ErrorContext(ctx, "auth: could not record that the first factor was proved",
			"user", userID, "error", err)
	}
}

// spendFactor checks an answer against what this person holds and, if it is
// right, spends it — in that order, and both halves in the caller's transaction.
//
// A six-digit answer is a TOTP and anything else of the right shape is a
// recovery code; anything else is ErrCredentials without a query. A TOTP is
// spent by moving last_step, which refuses a replay of the same step including
// from a second concurrent request, because the guard is in the UPDATE itself.
//
// It dispatches on the shape of *text*, and a passkey answer is never text: an
// assertion is a couple of hundred bytes of CBOR and has its own command, its own
// route and its own spend (passkeys.go). The two kinds share the list, the last-
// one refusal and the audit trail; they do not share a dispatcher, and this
// sentence is the difference between that and a trap.
func (s *Service) spendFactor(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID, answer string, at time.Time) (string, error) {
	code := normalizeCode(answer)
	switch {
	case len(code) == contracts.TOTPDigits && isDigits(code):
		return "totp", s.spendTOTP(tx, userID, code, at)
	case len(code) == recoveryCodeHex && isHex(code):
		return "recovery", s.spendRecoveryCode(ctx, tx, userID, code, at)
	default:
		usercontracts.EqualWork(code)
		return "", contracts.ErrCredentials
	}
}

func (s *Service) spendTOTP(tx db.Tx[db.Tenant], userID uuid.UUID, code string, at time.Time) error {
	key := s.factorKeyBytes()
	var rows []totpRow
	if err := tx.DB().Where("user_id = ?", userID).Order("created_at ASC").Find(&rows).Error; err != nil {
		return fmt.Errorf("auth: read the factors of %s: %w", userID, err)
	}
	if len(rows) == 0 {
		usercontracts.EqualWork(code)
		return contracts.ErrCredentials
	}
	for _, row := range rows {
		secret, err := openSecret(key, row.Secret)
		if err != nil {
			// A factor sealed with another key — a rotation of auth.factor_key,
			// or a row copied in from another deployment. It is not this person's
			// factor, and the answer to that is a new enrolment rather than a
			// session.
			continue
		}
		step, ok := totpMatches(secret, code, at, row.LastStep)
		if !ok {
			continue
		}
		res := tx.DB().Model(&totpRow{}).Where("id = ? AND last_step < ?", row.ID, step).
			Update("last_step", step)
		if res.Error != nil {
			return fmt.Errorf("auth: spend a factor code: %w", res.Error)
		}
		if res.RowsAffected == 1 {
			return nil
		}
		// Zero rows means the step was taken between the read and the write —
		// the replay, refused by the statement rather than by a comparison.
	}
	return contracts.ErrCredentials
}

func (s *Service) spendRecoveryCode(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID, code string, at time.Time) error {
	res := tx.DB().Model(&recoveryCodeRow{}).
		Where("user_id = ? AND code_hash = ? AND used_at IS NULL", userID, contracts.Hash(code)).
		Update("used_at", at)
	if res.Error != nil {
		return fmt.Errorf("auth: spend a recovery code: %w", res.Error)
	}
	if res.RowsAffected != 1 {
		return contracts.ErrCredentials
	}
	return events.Publish(ctx, tx, contracts.EventRecoveryCodeUsed, contracts.RecoveryCodeUsed{
		UserID: userID, At: at,
	})
}

// issueRecoveryCodes writes a fresh set and publishes that it did. It is the
// only place a recovery code is ever turned into bytes, and what reaches the
// database is the hash: the codes themselves go back to the caller to be shown
// once and are held in no row, no event and no notice.
func (s *Service) issueRecoveryCodes(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID) ([]string, error) {
	codes := make([]string, 0, contracts.RecoveryCodes)
	rows := make([]recoveryCodeRow, 0, contracts.RecoveryCodes)
	at := db.Now()
	for range contracts.RecoveryCodes {
		code, err := newRecoveryCode()
		if err != nil {
			return nil, err
		}
		codes = append(codes, code)
		// The hash is of the normalised form, because verification normalises
		// what a person typed before it compares: the hyphens are for the screen
		// that shows the code and for the chat window it gets pasted from, and a
		// code that only works when retyped exactly as printed is a code that
		// does not work.
		rows = append(rows, recoveryCodeRow{ID: uuid.New(), TenantID: db.TenantOf(tx).ID,
			UserID: userID, CodeHash: contracts.Hash(normalizeCode(code)), CreatedAt: at})
	}
	if err := tx.DB().Create(&rows).Error; err != nil {
		return nil, fmt.Errorf("auth: issue recovery codes: %w", err)
	}
	if err := events.Publish(ctx, tx, contracts.EventRecoveryCodesIssued, contracts.RecoveryCodesIssued{
		UserID: userID, Count: len(codes), At: at,
	}); err != nil {
		return nil, err
	}
	return codes, nil
}

// newRecoveryCode is 128 bits of crypto/rand in the one form a person can read
// off a screenshot: lower-case hex, hyphen-grouped in eights. The hyphens are
// stripped again by normalizeCode, so what is checked is what was issued with or
// without them.
func newRecoveryCode() (string, error) {
	raw := make([]byte, recoveryCodeBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("auth: mint a recovery code: %w", err)
	}
	full := hex.EncodeToString(raw)
	groups := make([]string, 0, 4)
	for i := 0; i < len(full); i += 8 {
		groups = append(groups, full[i:i+8])
	}
	return strings.Join(groups, "-"), nil
}

// A recovery code is 16 bytes: 128 bits, the same width as a session id, and
// 32 characters once hex-encoded — which is what it must be to match, since
// normalizeCode has already dropped the hyphens the printed form carries.
const (
	recoveryCodeBytes = 16
	recoveryCodeHex   = recoveryCodeBytes * 2
)

func isDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func isHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// factorEnrolled is Login's question: does signing this person in need the
// second thing as well as the first? Two indexed counts, asked after the password
// checked out and before anything was written.
//
// The passkey count is here, and so the "no factor key, so no factor" shortcut
// this function used to answer with is gone. A passkey writes no sealed secret, so
// a keyless deployment can and does hold passkeys, and an answer that stopped
// looking at the first table it cannot read would have told a person holding only
// passkeys that they needed no second factor — a silent lockout's twin, an open
// door. A deployment with no key and no rows still answers no, after one count over
// two empty indexed tables.
func (s *Service) factorEnrolled(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID) (bool, error) {
	var held int64
	if err := tx.DB().Model(&totpRow{}).Where("user_id = ?", userID).Count(&held).Error; err != nil {
		return false, fmt.Errorf("auth: ask whether %s has a factor: %w", userID, err)
	}
	if held > 0 {
		return true, nil
	}
	var keys int64
	if err := tx.DB().Model(&passkeyCredentialRow{}).Where("user_id = ?", userID).Count(&keys).Error; err != nil {
		return false, fmt.Errorf("auth: ask whether %s has a passkey: %w", userID, err)
	}
	return keys > 0, nil
}
