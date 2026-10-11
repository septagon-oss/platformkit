package internal

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/change/contracts"
)

// Tell is the module's reaction to its own two transitions, for the person who put the
// change forward: they are told what was decided, and what it did.
//
// It is a subscription and not a call inside Review and Apply, for the reason every
// subscription exists: the verdict and the notice are different promises, and the one
// that fails should not take the other with it. It is registered only when a composition
// hands over a Notifier — module.Module's Subscriptions list is built from Deps, so an
// installation that wired no delivery gets no subscription and the absence stays a
// written decision rather than a silent one.
//
// Two subscriptions, one per event, because kit/events' claim is per (event, durable):
// one handler for two names would make a redelivery of the second indistinguishable from
// the first.
func Tell(n contracts.Notifier, page func(id uuid.UUID) string) []events.Subscription {
	if n == nil {
		return nil
	}
	return []events.Subscription{
		tell(n, page, contracts.EventReviewed, contracts.NoticeReviewed, readReviewed),
		tell(n, page, contracts.EventApplied, contracts.NoticeApplied, readApplied),
	}
}

// readReviewed and readApplied are the two payloads reduced to the three facts a notice
// needs. Each names its own event in what it returns, because a payload that arrived
// under the wrong name is a bug and the sentence has to say which.
func readReviewed(payload []byte) (id, proposer uuid.UUID, err error) {
	var ev contracts.Reviewed
	if err := json.Unmarshal(payload, &ev); err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("change: read the decision: %w", err)
	}
	return ev.ProposalID, ev.Proposer, nil
}

func readApplied(payload []byte) (id, proposer uuid.UUID, err error) {
	var ev contracts.Applied
	if err := json.Unmarshal(payload, &ev); err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("change: read the applied change: %w", err)
	}
	return ev.ProposalID, ev.Proposer, nil
}

// tell is one of the two, over one payload shape.
//
// The recipient comes from the event's own proposer, which is the same value the row
// holds because the row wrote it: no body, no configuration and no role list names anybody
// who is told about a proposal. The proposal row is re-read here anyway — through the
// handler's transaction, under the tenant's own policy — because the summary a notice
// repeats has to be the row's, not a copy that arrived in a payload and may have been
// written by anything that could reach the outbox.
//
// A row that is gone is not a failure: there is nothing left to tell anybody about, the
// claim stands, and the delivery is over.
func tell(n contracts.Notifier, page func(id uuid.UUID) string, name, kind string,
	read func([]byte) (uuid.UUID, uuid.UUID, error)) events.Subscription {
	return events.Subscription{
		Module: "change", Name: name,
		Handler: func(ctx context.Context, tx db.Tx[db.Tenant], ev events.Event) error {
			id, proposer, err := read(ev.Payload)
			if err != nil {
				return err
			}
			if proposer == uuid.Nil {
				// A transition with no proposer on it is a payload bug, and the only safe
				// answer to "who is this for" when the answer is nobody is to tell nobody.
				return fmt.Errorf("change: %s names no proposer, so no notice is addressed", name)
			}
			row, err := crud.Get[*contracts.Proposal](tx, id)
			if err != nil {
				return err
			}
			link := ""
			if page != nil {
				link = page(row.ID)
			}
			return n.Told(ctx, tx, contracts.Notice{
				// Recipient, Summary and State are read off the row, not off the payload:
				// the row is the fact, it is read under this tenant's own policy, and a
				// payload is a copy that anything able to reach the outbox wrote.
				Recipient: row.Proposer, Kind: kind, ProposalID: row.ID,
				Summary: row.Summary, State: row.State, Link: link,
			})
		},
	}
}
