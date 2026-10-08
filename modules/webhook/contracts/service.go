package contracts

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
)

// FaultWindow is how long a failing endpoint keeps being retried: three days, as
// the brief asks for, counted from the first attempt of the event being retried.
// It is a contract rather than an internal constant because an administrator who
// reads "retried for 3 days" in the README is reading this number.
const FaultWindow = 72 * time.Hour

// MaxAttempts caps one event's deliveries at twelve, so a receiver that answers
// 500 forever costs twelve requests and not one request per tick forever. The
// window usually closes first; both are checked, and either one stops it.
const MaxAttempts = 12

// Issued is the answer of the two routes that mint a signing secret: the
// endpoint, and the secret in cleartext, once.
//
// It is a type of its own for the reason auth/contracts/tokens.go's IssuedToken
// is: a value whose only cleartext field is a credential must not be the type
// every read returns. Nothing here hands an Issued back twice — the create's
// response and the rotate's are the only two places the plaintext exists, and no
// route, screen, event, log line or delivery row can reach it.
type Issued struct {
	Endpoint *Endpoint `json:"endpoint"`
	// Secret is the whsec_ value, shown once. A caller who loses it rotates.
	Secret string `json:"secret"`
	// RotatedAt is when this secret became current, so a receiver can tell a
	// fresh secret from one it already stored.
	RotatedAt time.Time `json:"rotatedAt"`
}

// Service is everything about a webhook endpoint that generic CRUD cannot
// safely infer, because each command either moves the lifecycle, holds the
// secret, or sends.
//
// Every command takes the caller's transaction rather than opening one, so the
// row, the secret row, the dropped attempts and the outbox event are one commit,
// and every command locks the endpoint it is about with crud.GetForUpdate before
// it reads the state it is deciding on.
//
// The three errors a caller can act on are kit/crud's:
//
//	crud.ErrNotFound  no such endpoint in this tenant — the refusal a foreign id
//	                gets, which says nothing about whether the row exists
//	crud.ErrConflict  the endpoint is in a state that refuses this command
//	crud.ErrInvalid  the value is not one this module can deliver to
type Service interface {
	// Create registers an endpoint and mints its secret. It stamps the actor as
	// CreatedBy, refuses an address this deployment will not dial and an event
	// list with a name this composition does not emit, and writes the endpoint
	// row, the secret row and webhook.endpoint.created in one transaction.
	Create(ctx context.Context, tx db.Tx[db.Tenant], e *Endpoint) (*Issued, error)

	// Target changes where an endpoint delivers and what it is delivered. It is
	// a route and not a PATCH field because both re-check what the create
	// checked: the address policy with the deployment's flag, and the catalogue.
	// Changing neither the status nor the history, it writes the row and
	// webhook.endpoint.updated together.
	Target(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, url string, events EventNames) (*Endpoint, error)

	// Rotate mints a new secret and moves the current one to the grace window,
	// where it stays for GraceWindow so a delivery already in flight can still
	// be verified by a receiver that has not picked up the new one. The old
	// secret is never answered by any route.
	Rotate(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*Issued, error)

	// Enable takes a disabled endpoint back. It clears the fault clock, so
	// re-enabling is not a way to keep a broken endpoint looking alive.
	Enable(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*Endpoint, error)

	// Disable stops delivery now, drops this endpoint's queued attempts with a
	// reason, and publishes webhook.endpoint.disabled. Nothing it does deletes
	// a row: a disablement must not take the history away.
	Disable(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*Endpoint, error)

	// Replay is a person asking for one event to be sent again. It rechecks the
	// grant (the route's guard), the tenant scope (the row is loaded under row
	// level security, so a foreign id is ErrNotFound) and its own idempotence:
	// a pair that already has a queued row writes nothing and answers with that
	// row, and the row it writes is cause='replay', which is what tells a
	// replay apart from a retry in the log.
	Replay(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*Delivery, error)
}

// GraceWindow is how long a rotated-out secret keeps being *described* to a
// reader of the rotation record. The module signs with the current secret only;
// the previous ciphertext is kept for this long because a message already queued
// was addressed to a receiver holding it. Rotating twice inside one window drops
// the oldest, which is stated here rather than hidden in a column comment.
const GraceWindow = 24 * time.Hour

// SecretKeyBytes is the width of the deployment key that wraps an endpoint's
// signing secret: AES-256-GCM's own, so the one number the composition reads is
// the one number the seal needs. It is named here rather than in the package that
// seals, because the composition that reads the setting and the code that uses it
// are different packages and neither may own the other's constant.
const SecretKeyBytes = 32
