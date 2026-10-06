package contracts

import (
	"context"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
)

// The two things a proposal tells the person who wrote it. They are named for the
// transitions that cause them and not for a channel: this module has no idea whether a
// notice arrives as a row, an email, a push or nothing at all, and the composition that
// knows is the one that implements the port below.
const (
	NoticeReviewed = "reviewed"
	NoticeApplied  = "applied"
)

// Notifier is how a proposal tells a person something, without this module naming the
// module that owns delivery records. One method, addressed to one recipient, because the
// proposer is the only recipient a proposal row can name.
//
// Reviewers are told nothing by this module, and that is a decision rather than a gap: no
// port anywhere answers "who holds change:decide" — grants are a predicate over one
// caller, not a list of people — so a broadcast would have to guess a recipient set, and
// a guess is how one tenant's proposal summary reaches a person with no business reading
// it. Whoever composes a review queue and knows who its reviewers are notifies them from
// that knowledge, in their own code.
//
// Told runs inside the handler's transaction, so the notice commits with the claim on the
// event or not at all: a delivery that is claimed twice and a notice written twice are
// the same bug seen from either side, and kit/events' per-(event, durable) claim is what
// keeps both to one.
type Notifier interface {
	Told(ctx context.Context, tx db.Tx[db.Tenant], n Notice) error
}

// Notice is one decision about one proposal, addressed to the person who asked.
//
// It carries the summary, the state and a path, and never the diff. A value under review
// may be a price, a key, a contract term, and a copy of it in a notification row — which
// retention keeps, and which the mail worker renders into a message — would be a second,
// permanent, badly-scoped copy of the very thing the proposal exists to control. The
// reader who needs the value has the proposal page, at the Link, under their own grant.
type Notice struct {
	// Recipient is never anything but the row's proposer, read inside the handler.
	Recipient uuid.UUID
	// Kind is NoticeReviewed or NoticeApplied.
	Kind string
	// ProposalID, Summary and State are the row's own three readable facts.
	ProposalID uuid.UUID
	Summary    string
	State      string
	// Link is a path within this application, composed from the address the
	// composition serves the proposal page at.
	Link string
}

// ProposalPage is where one proposal is read: this application's own address for the
// page, with the row on the end. It is a port and not a constant because
// modules/change owns the proposal and not the address it answers at — the same reason
// the notice above carries a Link and no route — and a named type rather than a bare
// func because a composition hands it over as one named edge and a reader has to be
// able to see which edge it is.
type ProposalPage func(id uuid.UUID) string
