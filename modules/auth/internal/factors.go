package internal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm/clause"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
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

// ListFactors is what this person holds. Enrolled factors only: a secret that
// has proved nothing is not in the database to be listed.
func (s *Service) ListFactors(_ context.Context, tx db.Tx[db.Tenant], userID uuid.UUID) ([]*contracts.Factor, error) {
	var rows []totpRow
	if err := tx.DB().Where("user_id = ?", userID).Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("auth: list the factors of %s: %w", userID, err)
	}
	out := make([]*contracts.Factor, 0, len(rows))
	for _, row := range rows {
		out = append(out, &contracts.Factor{ID: row.ID, Kind: "totp", EnrolledAt: row.CreatedAt})
	}
	return out, nil
}

// WithdrawFactor ends one factor, and refuses to end the last.
//
// The count and the delete are one decision under one lock: SELECT ... FOR
// UPDATE over this person's factor rows takes every row a concurrent withdrawal
// could be deciding about, so two tabs each holding a different factor cannot
// both see "there are two" and leave the account with none. That is rule 8
// enforced by the database rather than by whoever wrote this function last.
func (s *Service) WithdrawFactor(ctx context.Context, tx db.Tx[db.Tenant], userID, factor uuid.UUID) error {
	var held []totpRow
	err := tx.DB().Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ?", userID).Find(&held).Error
	if err != nil {
		return fmt.Errorf("auth: read the factors of %s: %w", userID, err)
	}
	if !slices.ContainsFunc(held, func(row totpRow) bool { return row.ID == factor }) {
		return crud.ErrNotFound
	}
	if len(held) <= 1 {
		return contracts.ErrLastFactor
	}
	res := tx.DB().Where("id = ?", factor).Delete(&totpRow{})
	if res.Error != nil {
		return fmt.Errorf("auth: withdraw factor %s: %w", factor, res.Error)
	}
	if res.RowsAffected != 1 {
		return crud.ErrNotFound
	}
	return events.Publish(ctx, tx, contracts.EventFactorWithdrawn, contracts.FactorWithdrawn{
		UserID: userID, FactorID: factor, Kind: "totp", Remaining: len(held) - 1, At: db.Now(),
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
	if held == 0 {
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

// spendFactor checks an answer against what this person holds and, if it is
// right, spends it — in that order, and both halves in the caller's transaction.
//
// A six-digit answer is a TOTP and anything else of the right shape is a
// recovery code; anything else is ErrCredentials without a query. A TOTP is
// spent by moving last_step, which refuses a replay of the same step including
// from a second concurrent request, because the guard is in the UPDATE itself.
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
// second thing as well as the first? One indexed count, asked after the password
// checked out and before anything was written.
func (s *Service) factorEnrolled(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID) (bool, error) {
	if len(s.factorKeyBytes()) == 0 {
		// No key, so no factor was ever written and none can be: the answer is
		// no without a query, and a deployment that set no key signs in exactly
		// as it did before this table existed.
		return false, nil
	}
	var held int64
	if err := tx.DB().Model(&totpRow{}).Where("user_id = ?", userID).Count(&held).Error; err != nil {
		return false, fmt.Errorf("auth: ask whether %s has a factor: %w", userID, err)
	}
	return held > 0, nil
}
