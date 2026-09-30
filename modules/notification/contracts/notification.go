// Package contracts is everything another module, an app or a test may know
// about notifications: the entity, the notice, the events, the two interfaces
// this module needs somebody else to satisfy, and the Service. The
// implementation is in ../internal.
//
// A notification is a row addressed to one person, and optionally an email.
// Both come from one call — Notify — because "tell somebody" is one intention,
// and a caller that had to write the row and then send the mail would be a
// caller that can do half of it.
package contracts

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
)

// Notification is one thing one person was told, in one tenant. crud.Base
// contributes the id, the timestamps, the soft delete and the tenant column
// row-level security matches on; RecipientID is a user id with no foreign key
// behind it, which is what "cross-module dependencies are Go interfaces" costs
// at the database.
type Notification struct {
	crud.Base

	// RecipientID is who was told. A caller who could re-address a notice
	// could read somebody else's, so nothing writes it but Notify.
	RecipientID uuid.UUID `json:"recipientId" gorm:"type:uuid;not null" format:"uuid" doc:"The person this is for"`
	// Title is the one line a bell shows; Body is the rest.
	Title string `json:"title" gorm:"type:text;not null" validate:"required" maxLength:"200" doc:"One-line summary" example:"A task was assigned to you"`
	Body  string `json:"body,omitempty" gorm:"type:text;not null;default:''" ui:"widget:textarea" doc:"The rest of the message"`
	// Link is a path within the application and not a URL: an absolute one is
	// a notice somebody can use to send a tenant's users elsewhere.
	Link string `json:"link,omitempty" gorm:"type:text;not null;default:''" maxLength:"500" doc:"Path this notice points at" example:"/admin/task/tasks/1"`
	// ReadAt is when the recipient saw it, nil until they have — a timestamp
	// rather than a flag, because "when" is what a support conversation asks.
	ReadAt *time.Time `json:"readAt,omitempty" gorm:"type:timestamptz" doc:"When the recipient read it" readOnly:"true"`
}

// TableName pins the table, so the entity and migrations/000011 agree.
func (Notification) TableName() string { return "notifications" }

// Validate is the entity's own check, run by kit/crud on every write whichever
// door it came through.
func (n *Notification) Validate(context.Context) error {
	n.Title = strings.TrimSpace(n.Title)
	n.Link = strings.TrimSpace(n.Link)
	switch {
	case n.RecipientID == uuid.Nil:
		return fmt.Errorf("a notification is addressed to somebody")
	case n.Title == "":
		return fmt.Errorf("a notification needs a title")
	case n.Link != "" && !strings.HasPrefix(n.Link, "/"):
		return fmt.Errorf("a link is a path within this application, and %q is not", n.Link)
	}
	return nil
}

// Notice is what a caller wants somebody told. It is the argument to Notify
// rather than the entity, because the entity has an id, timestamps and a tenant
// the server owns, and because what a caller asks for is a request and not a
// promise: Wants is a ceiling, and the tenant's sender, the tenant's switches
// and the person's own preferences decide which channels happen (Decide). A
// notice that asks for three channels and gets one row in the ledger for each of
// them has been accounted for even where two of them were refused.
type Notice struct {
	Recipient uuid.UUID
	// Intent names what happened, and it selects the template (per event, per
	// locale) and the row of the person's preferences that applies. It is the
	// caller's word, not a table this module keeps: "task.assigned" means
	// something to the task module and nothing to this one, which is what lets
	// a new kind of notice arrive without a change here. "" is a notice with no
	// event of its own — the generic template, the blanket preference.
	Intent string
	// Class is what kind of notice this is, and today there are two answers:
	// ClassStandard, and ClassSecurity, which is not subject to the person's
	// opt-out or their quiet hours. The caller says which it is; this module
	// never guesses from Intent, because Intent is the caller's vocabulary.
	Class string
	Title string
	Body  string
	Link  string
	// Wants is the ceiling: which channels the caller would accept. Its zero
	// value is in-app alone — the floor every notice has — so a caller that says
	// nothing tells the person in the application and contacts nobody.
	Wants Wants
}

// The two classes of notice. A class is not a channel and not an event: it is
// how much this module obeys the person's settings about this one notice.
const (
	// ClassStandard is an ordinary notice: preferences and quiet hours decide.
	ClassStandard = ""
	// ClassSecurity is a notice about the person's own account — a password
	// changed, a new session, a token about to expire. It is sent over the
	// person's opt-out and outside their quiet hours, because opt-out exists so
	// that people are not marketed to and not so that they cannot be told
	// somebody took over their account. It is still suppressed where the
	// channel itself cannot be used at all (no address, no verified sender,
	// no provider), because that is not a choice this module is refusing.
	ClassSecurity = "security"
)

// RecipientLookup is how this module turns a user id into an address without
// naming the user module. The application satisfies it — apps/platformkit
// adapts user/contracts.Service in four lines — which is what keeps the
// dependency pointing from the app at both modules rather than from one at the
// other. It returns crud.ErrNotFound for somebody who is not there and "" for
// somebody with no address; neither is an error to the caller.
type RecipientLookup interface {
	Email(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID) (string, error)
}

// HostLookup is how this module turns a tenant into the host its people reach
// the application at, without naming the module that knows.
//
// A mail client has no base to resolve a path against, so a notice's link has
// to become a URL somewhere — and the URL has to be the recipient's own host.
// Every tenant is reached at its own name, so a link built from the
// application's public host is a link that signs nobody in and, worse, sends
// one customer's people to another customer's front door.
//
// It takes the worker's own tenant transaction, so the answer is read under the
// tenant's own policy: the row it looks for is the one row of tenant_hosts that
// transaction can see. The application satisfies it over the tenant module, the
// way it satisfies RecipientLookup over the user module.
type HostLookup interface {
	PublicHost(ctx context.Context, tx db.Tx[db.Tenant]) (string, error)
}

// Message is one email, already rendered: the whole of what a Mailer is asked.
//
// It carries the sender rather than relying on the mailer to know one, because
// the mailer is one object shared by every tenant this process serves: the name
// in a header belongs to one of them, so it arrives with the message that needs
// it. A nil Sender is the deployment's own address and no display name — which
// is exactly what config.Mail.From still is, and still refuses to be anything
// else.
type Message struct {
	To string
	// Sender is the tenant's own: its name, its address, and the key to sign
	// with. Nil means send as the deployment.
	Sender *Sender
	// ReplyTo is where an answer goes, "" for the sender's own address.
	ReplyTo string
	Subject string
	// Body is the plain-text message and HTML its alternative, both rendered in
	// Lang. A message with no HTML is sent as text alone; a message with no
	// text is refused, because a mail client that shows nobody a raw HTML
	// source is the failure the text part exists to prevent.
	Body string
	HTML string
	// Lang is the BCP-47 language Body and HTML are in, so the message says
	// what it is written in.
	Lang string
}

// Mailer sends one message. There is one production implementation, SMTP, and
// one in memory — notification.Mailbox — that a test and an unconfigured
// deployment both use; a second production sender would be a second thing to
// keep working for no capability the first does not have.
//
// It is exported rather than internal to this module because it has a second
// consumer, and the exception is worth naming. Everything this application
// mails goes out of the worker below, which reads a notification row back and
// renders it — so whatever is in the message is, by construction, in a row.
// That is right for every notice there is and wrong for exactly one thing: a
// set-password link. modules/auth is handed this same Mailer by the
// composition, mints the token in its own subscription and hands the message
// over directly, so the secret is in the mail and in no row, no outbox payload
// and no audit event. The alternative was a live credential sitting in
// notifications.link, which is what it used to be.
type Mailer interface {
	Send(ctx context.Context, m Message) error
}

// Service is what a caller does with notifications. Every command takes the
// caller's transaction rather than opening one, so the row and the events it
// causes commit together; the errors are kit/crud's.
type Service interface {
	// Notify writes the row, publishes notification.created, and — when the
	// notice asks for mail and the recipient has an address — publishes
	// notification.email_requested. Nothing here talks to a mail server: a
	// request that waited on one would hold a database transaction open across
	// a call to somebody else's machine.
	Notify(ctx context.Context, tx db.Tx[db.Tenant], n Notice) (*Notification, error)

	// MarkRead records that the recipient has seen it and publishes
	// notification.read. Somebody else's notification is not found, which is
	// the check that makes the route safe for any signed-in caller; marking it
	// read again changes nothing and publishes nothing.
	MarkRead(ctx context.Context, tx db.Tx[db.Tenant], id, recipient uuid.UUID) (*Notification, error)

	// ListFor is a page of one person's notifications, newest first. There is
	// no tenant-wide list, which is why this module mounts no rest.Spec.
	ListFor(ctx context.Context, tx db.Tx[db.Tenant], recipient uuid.UUID, q crud.Query) ([]*Notification, int64, error)
}
