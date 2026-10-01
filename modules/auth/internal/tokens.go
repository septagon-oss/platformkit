package internal

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// The bearer token. contracts.Tokens says what each command decides; this file
// decides it, and Authenticate's bearer leg reads what it wrote.
//
// One rule runs through all of it: a token is a *narrowed* credential, never an
// escalated one. Its scopes are checked against what every module declares and
// against what the holder's own roles already grant, both before any row exists,
// and the effective set is recomputed from the roles on every request — so
// standing somebody down from a role narrows the keys they minted, the way the
// audit trail says it should have.

var _ contracts.Tokens = (*Service)(nil)

// apiTokenRow is one issued key. The row is keyed by the hash of the token for
// the reason sessions are (see 000032_api_tokens.up.sql), and the token itself
// exists in exactly one response.
type apiTokenRow struct {
	ID         uuid.UUID `gorm:"primaryKey"`
	TenantID   uuid.UUID
	UserID     uuid.UUID
	CreatedBy  uuid.UUID
	Name       string                `gorm:"column:name"`
	Scopes     contracts.Permissions `gorm:"type:text[]"`
	TokenHash  []byte
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastUsedAt time.Time
	RevokedAt  *time.Time
}

func (apiTokenRow) TableName() string { return "api_tokens" }

// IssueToken mints a key, having refused anything it cannot honestly hand over.
//
// The order is the point: ask what the person's roles grant, check the requested
// scopes against the catalogue and against that answer, and only then write. A
// refused issue writes no row and publishes no event, and returns nothing — there
// is no half-issued key to report.
func (s *Service) IssueToken(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID, in contracts.TokenIntent, declared []tenancy.Grant) (*contracts.IssuedToken, error) {
	if in.Name == "" || len(in.Name) > 80 || len(in.Scopes) == 0 {
		return nil, fmt.Errorf("auth: a token needs a name and at least one scope: %w", crud.ErrInvalid)
	}
	user, err := s.users.Get(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	if user.Status != usercontracts.StatusActive {
		return nil, contracts.ErrCredentials
	}
	// The same two checks a role write gets, for the same reason: a scope nothing
	// defines can never be exercised, and a scope the holder does not have would
	// be a credential that reads as authority and is not.
	granted, err := contracts.CheckedPermissions(in.Scopes, declared, db.TenantOf(tx))
	if err != nil {
		return nil, err
	}
	held, err := s.Permissions(ctx, tx, []string(user.Roles))
	if err != nil {
		return nil, err
	}
	for _, scope := range granted {
		// The same predicate the authorizer uses, from the same package, rather
		// than a list comparison written again here: whether a holder has
		// something is not one question but two, because the wildcard answers for
		// every permission in the tenant and for no operator permission at all.
		// Without this an administrator could mint no key whatsoever — their held
		// list names the wildcard and never the narrow thing they were asked for.
		if string(scope) == contracts.Wildcard || !contracts.Grants(held, grantFor(declared, string(scope))) {
			return nil, fmt.Errorf("auth: %s is not this holder's to delegate: %w", scope, contracts.ErrTokenScope)
		}
	}
	at := db.Now()
	until := in.ExpiresAt
	if until.IsZero() {
		until = at.Add(contracts.APITokenDefaultLifetime)
	}
	if until.After(at.Add(contracts.APITokenMaxLifetime)) {
		return nil, fmt.Errorf("auth: a token may not outlive %s: %w", contracts.APITokenMaxLifetime, crud.ErrInvalid)
	}
	token, hash, err := newToken()
	if err != nil {
		return nil, err
	}
	row := apiTokenRow{ID: uuid.New(), TenantID: db.TenantOf(tx).ID, UserID: userID, CreatedBy: userID,
		Name: in.Name, Scopes: slices.Clone(granted), TokenHash: hash,
		CreatedAt: at, ExpiresAt: until, LastUsedAt: at}
	if err := tx.DB().Create(&row).Error; err != nil {
		return nil, fmt.Errorf("auth: issue a token: %w", err)
	}
	// The event names the key and its scope, never its bytes: the trail is
	// readable by people who must not be able to use it.
	if err := events.Publish(ctx, tx, contracts.EventAPITokenIssued, contracts.APITokenIssued{
		TokenID: row.ID, Name: row.Name, Scopes: []string(row.Scopes), ExpiresAt: until, At: at,
	}); err != nil {
		return nil, err
	}
	return &contracts.IssuedToken{
		Token: token,
		APIToken: contracts.APIToken{ID: row.ID, Name: row.Name, Scopes: row.Scopes,
			CreatedAt: at, ExpiresAt: until, LastUsedAt: at, CreatedBy: userID},
	}, nil
}

// ListTokens is the screen: which keys exist, what they may do, when they die,
// which one is already stopped. Not one of them is a credential.
func (s *Service) ListTokens(_ context.Context, tx db.Tx[db.Tenant], userID uuid.UUID) ([]*contracts.APIToken, error) {
	var rows []apiTokenRow
	if err := tx.DB().Where("user_id = ?", userID).Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("auth: list the tokens of %s: %w", userID, err)
	}
	out := make([]*contracts.APIToken, 0, len(rows))
	for _, row := range rows {
		out = append(out, &contracts.APIToken{
			ID: row.ID, Name: row.Name, Scopes: []string(row.Scopes), CreatedAt: row.CreatedAt,
			ExpiresAt: row.ExpiresAt, LastUsedAt: row.LastUsedAt, CreatedBy: row.CreatedBy,
			RevokedAt: row.RevokedAt,
		})
	}
	return out, nil
}

// RevokeToken stops one key, and says nothing about a key it cannot see.
func (s *Service) RevokeToken(ctx context.Context, tx db.Tx[db.Tenant], userID, id uuid.UUID) error {
	var row apiTokenRow
	err := crud.Classify(tx.DB().Where("id = ? AND user_id = ?", id, userID).Take(&row).Error)
	if err != nil {
		return err
	}
	if row.RevokedAt != nil {
		// Already stopped. The caller wanted this and it is true, which is the
		// same rule Logout follows — and re-publishing it would put two entries
		// in the trail for one decision.
		return nil
	}
	at := db.Now()
	res := tx.DB().Model(&apiTokenRow{}).Where("id = ? AND revoked_at IS NULL", id).
		Update("revoked_at", at)
	if res.Error != nil {
		return fmt.Errorf("auth: revoke a token: %w", res.Error)
	}
	if res.RowsAffected != 1 {
		return crud.ErrNotFound
	}
	return events.Publish(ctx, tx, contracts.EventAPITokenRevoked, contracts.APITokenRevoked{
		TokenID: id, Name: row.Name, ExpiresAt: row.ExpiresAt, LastUsedAt: row.LastUsedAt, At: at,
	})
}

// bearerCaller is what a token buys: the person it belongs to, and the ceiling
// they may act inside. Nil, with no error, means this token is nobody — revoked,
// expired, spent, or one that was never issued here — and every one of those is
// the same answer at the same cost, because a caller who cannot present a working
// key gains nothing from learning which kind of failure they met.
//
// The scopes are rechecked against the roles on every request rather than read
// back off the row: standing somebody down from a role must narrow the keys they
// minted, in the same request that did it, and a cache of the intersection would
// be a list of decisions the platform has already reversed.
func (s *Service) bearerCaller(ctx context.Context, tx db.Tx[db.Tenant], token string) (user *usercontracts.User, scopes []string, ok bool, err error) {
	// The prefix is part of the string a caller holds, and not part of what is
	// hashed: it says what the secret is, so a scanner reading a log can tell
	// this from a session id, and a person can tell which way to paste it.
	body, found := strings.CutPrefix(token, tokenPrefix)
	if !found {
		return nil, nil, false, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil || len(raw) != tokenBytes {
		return nil, nil, false, nil
	}
	var row apiTokenRow
	switch err := crud.Classify(tx.DB().Where("token_hash = ?", contracts.Hash(string(raw))).Take(&row).Error); {
	case errors.Is(err, crud.ErrNotFound):
		return nil, nil, false, nil
	case err != nil:
		return nil, nil, false, err
	}
	at := db.Now()
	if row.RevokedAt != nil || !row.ExpiresAt.After(at) {
		return nil, nil, false, nil
	}
	user, err = s.users.Get(ctx, tx, row.UserID)
	if err != nil {
		if errors.Is(err, crud.ErrNotFound) {
			return nil, nil, false, nil
		}
		return nil, nil, false, err
	}
	if user.Status != usercontracts.StatusActive {
		return nil, nil, false, nil
	}
	held, err := s.Permissions(ctx, tx, []string(user.Roles))
	if err != nil {
		return nil, nil, false, err
	}
	// The intersection, in the order the key was issued with, and never wider
	// than the row: the roles decide what a scope still means.
	scopes = make([]string, 0, len(row.Scopes))
	for _, scope := range row.Scopes {
		// The same predicate as at issue: a holder with the wildcard has every
		// scope their key names, and a holder who has been stood down from the
		// role that named it has none.
		if contracts.Grants(held, tenancy.Grant{Permission: scope}) {
			scopes = append(scopes, scope)
		}
	}
	// The touch, throttled: a key used on every request of a mobile session is
	// not a reason for a row lock on each one. last_used_at is what a person
	// reads, and it never moves expires_at.
	if row.LastUsedAt.Add(contracts.APITokenTouch).Before(at) {
		if err := tx.DB().Model(&apiTokenRow{}).Where("id = ?", row.ID).
			Update("last_used_at", at).Error; err != nil {
			return nil, nil, false, fmt.Errorf("auth: note the use of a token: %w", err)
		}
	}
	return user, scopes, true, nil
}

const (
	tokenBytes  = 32
	tokenPrefix = "pkit_"
)

// grantFor is the catalogue's own entry for a permission, so the operator flag
// travels with the question: an operator permission is granted only by being
// named, and asking the question without that flag would answer it wrongly. A
// permission nothing declares (revoked since this key was minted) is asked about
// as an ordinary one, which is the narrower — that is, safer — reading.
func grantFor(declared []tenancy.Grant, permission string) tenancy.Grant {
	for _, g := range declared {
		if g.Permission == permission {
			return g
		}
	}
	return tenancy.Grant{Permission: permission}
}

// newToken is the credential and the key it is stored under. The prefix marks
// what the string is, so a secret scanner can tell this from a session id or a
// mail token in a log it is reading; the entropy is 256 bits of crypto/rand and
// the encoding is the one that survives a URL and a shell.
func newToken() (token string, hash []byte, err error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("auth: mint a token: %w", err)
	}
	return tokenPrefix + base64.RawURLEncoding.EncodeToString(raw), contracts.Hash(string(raw)), nil
}
