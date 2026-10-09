package contracts

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
)

// The two doors' permissions (ADR 0020 §7). `subject:export` is the operator's door onto
// anybody's bundle; the person's own export asks no permission, because Article 15 is a
// right and cannot require a grant a person might not hold — that route declares
// `httpx.SignedIn` instead, and the difference is the whole of the row in the table.
const (
	PermissionExport = "subject:export"
	PermissionErase  = "subject:erase"
)

// The two facts the door publishes. Neither names the person by an identifier: an event
// carrying an address would copy it into the outbox and the trail, which is the opposite
// of the module that emitted it — the same rule `Erased` follows.
const (
	EventExportRequested = "subject.export_requested"
	EventExportReady     = "subject.export_ready"
)

// What a request is and what it is waiting for. The state is the worker's queue: a row in
// `requested` is the whole of what there is to do, and the row that reaches a terminal
// state says which way it ended.
const (
	KindExport = "export"
	KindErase  = "erase"

	StateRequested = "requested"
	StateRunning   = "running"
	StateReady     = "ready"
	StateErased    = "erased"
	StateFailed    = "failed"
)

// LinkLife is the life of a minted bundle link: seven days, single use, hashed at rest
// (ADR 0020 §6). It is longer than `file.MaxGrantExpiry` on purpose and by that ADR's
// decision — the file module's ceiling is the one a product's uploads live under, and a
// person's right to their own copy is one command's.
const LinkLife = 7 * 24 * time.Hour

// MaxReason bounds what a person may type into the reason box. It is the same shape as
// `file.MaxErasureReason` for the same reason: the text travels into a row and into the
// bundle's manifest, and an unbounded field is a storage bill rather than a right.
const MaxReason = 500

// ErrGrant is the answer of a link that is not open. Expired, already used and never
// issued are one answer, and saying which would be a oracle: the refusal is the same for
// all three because the caller who holds a wrong token learns nothing useful either way
// (ADR 0020 §7's last row).
var ErrGrant = errors.New("subject: that link is not open")

// RequestExportInput is one ask for a person's copy.
type RequestExportInput struct {
	// Subject is the person whose rows are wanted. On the person's own door the route
	// fills it from the session and refuses a body that names somebody else; on the
	// operator's door it comes from the request and is what `subject:export` is for.
	Subject uuid.UUID
	// Reason is why the person is asking, in their words. It buys nothing and proves
	// nothing — the right is unconditional — and it is recorded because a support
	// conversation three weeks later is about this sentence.
	Reason string
}

// Request is one door asked for, and the answer to "where is my export".
type Request struct {
	ID uuid.UUID `json:"id" gorm:"column:id;type:uuid;primaryKey" format:"uuid"`
	// Tenant is the row's bound, stamped by the command from the transaction it ran in and
	// never read from the caller — the rule kit/crud follows for every entity, here in a
	// line of code because this row is written by hand. The policy's WITH CHECK refuses a
	// row stamped for another tenant whatever this field says.
	Tenant    uuid.UUID `json:"-" gorm:"column:tenant_id;type:uuid"`
	CreatedAt time.Time `json:"createdAt" gorm:"column:created_at" format:"date-time"`
	UpdatedAt time.Time `json:"updatedAt" gorm:"column:updated_at" format:"date-time"`
	Subject   uuid.UUID `json:"subject" gorm:"column:subject_id;type:uuid" format:"uuid"`
	// Requester is whoever asked. It is a column and not inferred from the trail so the
	// door can answer "yours or somebody else's" without leaving its own table.
	Requester uuid.UUID `json:"requester" gorm:"column:requested_by;type:uuid" format:"uuid"`
	Kind      string    `json:"kind" gorm:"column:kind"`
	State     string    `json:"state" gorm:"column:state"`
	Reason    string    `json:"reason,omitempty" gorm:"column:reason"`
	// Detail is the failure, in the words of whoever failed: the module that said no to
	// an erasure, or the error a bundle died on. Empty while the work is open.
	Detail string `json:"detail,omitempty" gorm:"column:detail"`
	// Bundle names the file row that holds the zip and the digest it was checked at.
	// The nil uuid and the empty string mean there is no bundle yet.
	Bundle       uuid.UUID  `json:"bundle" gorm:"column:bundle_file_id;type:uuid" format:"uuid"`
	BundleDigest string     `json:"bundleDigest,omitempty" gorm:"column:bundle_digest"`
	LinkExpires  *time.Time `json:"linkExpires,omitempty" gorm:"column:link_expires_at" format:"date-time"`
	// Revision is this row's own, which every write rechecks inside its transaction.
	Revision int64 `json:"revision" gorm:"column:revision"`
}

// TableName pins the table migrations/000001 creates.
func (Request) TableName() string { return "subject_requests" }

// ExportRequested is EventExportRequested's payload: the request exists, and the worker
// has work to do.
type ExportRequested struct {
	Request   uuid.UUID `json:"request"`
	Subject   uuid.UUID `json:"subject"`
	Requester uuid.UUID `json:"requester"`
	At        time.Time `json:"at"`
}

// ExportReady is EventExportReady's payload. It names the bundle and never carries the
// link: a token in an event is a token in the outbox, the trail and every subscriber's
// store, and the seven-day door is meant to be held by one person.
type ExportReady struct {
	Request   uuid.UUID `json:"request"`
	Subject   uuid.UUID `json:"subject"`
	Bundle    uuid.UUID `json:"bundle"`
	Digest    string    `json:"digest"`
	ExpiresAt time.Time `json:"expiresAt"`
	At        time.Time `json:"at"`
}

// Link is one minted door onto a bundle. Only the caller of Mint ever sees Token; what
// the database keeps is its hash (see subject_grants).
type Link struct {
	Request   uuid.UUID `json:"request"`
	Subject   uuid.UUID `json:"subject"`
	File      uuid.UUID `json:"file" format:"uuid"`
	Token     string    `json:"token" doc:"The one time this token exists outside the database. Mint hands it to one caller and the table holds only its hash."`
	ExpiresAt time.Time `json:"expiresAt" format:"date-time"`
}

// Grant is one minted link as it is stored. The token itself is never in a column:
// TokenHash is what a reader of this table gets, and a hash of an unguessable token opens
// nothing (ADR 0020 §6, and the rule sessions and password tokens follow).
type Grant struct {
	ID         uuid.UUID  `gorm:"column:id;type:uuid;primaryKey"`
	Tenant     uuid.UUID  `gorm:"column:tenant_id;type:uuid"`
	CreatedAt  time.Time  `gorm:"column:created_at"`
	Request    uuid.UUID  `gorm:"column:request_id;type:uuid"`
	TokenHash  string     `gorm:"column:token_hash"`
	ExpiresAt  time.Time  `gorm:"column:expires_at"`
	RedeemedAt *time.Time `gorm:"column:redeemed_at"`
}

// TableName pins the table migrations/000001 creates.
func (Grant) TableName() string { return "subject_grants" }

// NewToken mints the 32 random bytes a link is made of and returns the token a caller
// sends with their request beside the hash this module stores. Both come from here so the
// two can never disagree about the encoding: a link whose stored hash is of a different
// spelling than the one handed out is a link that never opens.
func NewToken() (token, hash string, err error) {
	var raw [32]byte
	if _, err = rand.Read(raw[:]); err != nil {
		return "", "", fmt.Errorf("subject: mint a bundle token: %w", err)
	}
	token = hex.EncodeToString(raw[:])
	return token, HashToken(token), nil
}

// HashToken is the shape a token is stored in: sha256 in hex, the rule sessions and
// password tokens follow. A reader of this table gets hashes, and a hash of an unguessable
// token opens nothing.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Door is the two doors as a caller reaches them: a request that commits, work the worker
// does later, and the link that lets one person open what the work produced. The two
// commands modules/subject has always answered — Export and Erase — are on Service; these
// are the ones that need a row, a queue and a file.
type Door interface {
	// RequestExport writes one `requested` row and publishes subject.export_requested in
	// the same transaction. It builds nothing: a request that walked nine owners inline
	// would be a request that times out, and the person would not know whether to retry.
	// A reason past MaxReason is refused before anything is written.
	RequestExport(ctx context.Context, tx db.Tx[db.Tenant], in RequestExportInput) (*Request, error)

	// RequestErase runs the erasure in the caller's transaction and files the receipt
	// beside it. The row is unique on the person, so the request that arrives a second
	// time answers the first one's row instead of scrubbing anything twice.
	RequestErase(ctx context.Context, tx db.Tx[db.Tenant], in EraseInput) (*Request, *Erasure, error)

	// Request reads one request row back: "where is my export", answered to the person who
	// asked it, or to a holder of subject:export. Somebody else's row is not found, which is
	// the answer that does not confirm it exists.
	Request(ctx context.Context, tx db.Tx[db.Tenant], request, actor uuid.UUID, operator bool) (*Request, error)

	// Mint opens a seven-day, single-use door onto one request's bundle, for the person who
	// asked or for a holder of subject:export. The token is in the answer and nowhere else.
	Mint(ctx context.Context, tx db.Tx[db.Tenant], request, actor uuid.UUID, operator bool) (*Link, error)

	// Redeem spends a token: it answers the bundle the link points at, and the row it
	// reads is the row that says the token was used. Unknown, expired and spent all answer
	// ErrGrant, and none of them writes anything.
	Redeem(ctx context.Context, tx db.Tx[db.Tenant], token string) (*Link, error)
}

// BundleSource is the one read the door makes of the file module: enough to store a
// bundle and hold it while its link lives. It is declared here rather than importing
// `filecontracts.Service` so the door's dependency is the two calls it makes, and the
// composition hands over the file module's own Service, which satisfies it.
type BundleSource interface {
	// Put stores one bundle: private, and named for the request it answers. It answers the
	// file row's id.
	Put(ctx context.Context, tx Tx, name string, body []byte) (uuid.UUID, error)

	// Hold keeps one stored bundle against the retention sweep until `until`. The file
	// module's own Retain "places or replaces" a hold, which is what makes the two calls
	// rather than one: the walk holds the bundle from the moment it made it, and every
	// link minted from it moves the end of the hold to the end of that link's own life.
	// One call at production time would either sweep bytes under an open link or hold them
	// forever, and a person whose copy was deleted while their link worked has no answer.
	Hold(ctx context.Context, tx db.Tx[db.Tenant], file uuid.UUID, until time.Time) error
}

// Tx opens the transaction the file row is written in. It is this package's own spelling
// of `filecontracts.Tx` — a function the file module calls once the bytes are in storage
// — kept here so contracts/ names no storage SDK.
type Tx func(ctx context.Context) (db.Tx[db.Tenant], error)

// Notifier is the door's half of a person's inbox: one notice that the bundle is ready.
// It is a port and not `notificationcontracts.Service` because what the door needs is one
// sentence, and a dependency the size of a service is a dependency a reader has to audit.
// The in-app notice says *an export is ready* and points at the request; the minted token
// is never in it, because a link in a row is a link a database read hands out
// (ADR 0020 §6, and modules/notification's own rule that a secret does not sit in a row).
type Notifier interface {
	Ready(ctx context.Context, tx db.Tx[db.Tenant], notice Notice) error
}

// Notice is what the person is told.
type Notice struct {
	Recipient uuid.UUID
	Title     string
	Body      string
	// Link is a path inside the application — the request the notice is about — and never
	// a token.
	Link string
}
