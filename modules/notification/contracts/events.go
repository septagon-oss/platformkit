package contracts

import (
	"time"

	"github.com/google/uuid"
)

// The three events this module emits. A subscriber names one of these constants
// rather than a string, so renaming one is a compile error where it is listened
// for.
//
// EventEmailRequested is the odd one, and it is the point of this module's
// shape: it is how "and send it by mail" leaves the request. The row and the
// event commit together in the caller's transaction and the worker renders and
// sends, so a request never waits on somebody else's machine, a mail server
// that is down is retried by the outbox, and a message that can never be sent
// ends in the kernel's dead letters where somebody can read it.
const (
	EventCreated        = "notification.created"
	EventEmailRequested = "notification.email_requested"
	EventRead           = "notification.read"
)

// Events is every event this module emits, for the manifest.
var Events = []string{EventCreated, EventEmailRequested, EventRead}

// Created is the payload of EventCreated: somebody was told something.
type Created struct {
	NotificationID uuid.UUID `json:"notificationId"`
	Recipient      uuid.UUID `json:"recipientId"`
	Title          string    `json:"title"`
	At             time.Time `json:"at"`
}

// EmailRequested is the payload of EventEmailRequested, and it carries two
// identifiers and nothing else.
//
// It used to carry the whole message — the address, the title, the body, the
// link — which saved the worker a query and cost something worth more than the
// query. An outbox row is kept for a week and copied into every subscriber's
// trail: modules/audit records every event this application publishes, so a
// payload with a body in it is the body of every notice in a table nobody
// treats as a mailbox, and a payload with an address in it is a mailing list.
// A reset link in one would be a live credential in the audit trail.
//
// So the worker reads the row back inside the event's own tenant transaction
// and resolves the address there. The consequences are named rather than
// hidden: a notice deleted before the worker reaches it is a skip and a log
// line, and an address changed in between is the address the mail goes to,
// which is the newer of the two answers and the one a person expects.
//
// The convention this makes explicit for one event is the one modules/audit
// relies on for all of them: an event carries identifiers, not content.
type EmailRequested struct {
	NotificationID uuid.UUID `json:"notificationId"`
	Recipient      uuid.UUID `json:"recipientId"`
	At             time.Time `json:"at"`
}

// Read is the payload of EventRead: the recipient has seen it.
type Read struct {
	NotificationID uuid.UUID `json:"notificationId"`
	Recipient      uuid.UUID `json:"recipientId"`
	At             time.Time `json:"at"`
}
