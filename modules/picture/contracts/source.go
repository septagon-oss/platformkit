package contracts

import (
	"context"
	"errors"

	"github.com/septagon-oss/platformkit/kit/db"
)

// PictureSource is the authenticated delivery address; {reference} is one canonical token.
const PictureSource = "/api/v1/picture/pictures/{reference}/content"

var (
	// ErrUnavailable hides missing, deleted, private, and wrong-tenant sources as one refusal.
	ErrUnavailable = errors.New("picture: source is unavailable")
	// ErrTemporary reports a retryable source or storage failure without exposing its details.
	ErrTemporary = errors.New("picture: source is temporarily unavailable")
)

// Input is a detached, freshly authorized snapshot of the current source facts.
type Input struct {
	TemplateDigest [32]byte
	Value          Value
}

// Source authorizes the active actor and reads consistent facts in the supplied
// tenant transaction on every call. It returns zero Input on refusal; it must
// never cache grants, open a second transaction, or fall back to system scope.
type Source interface {
	Load(context.Context, db.Tx[db.Tenant], string) (Input, error)
}
