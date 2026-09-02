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

// EmailRequested is the payload of EventEmailRequested, and it carries the
// whole message rather than an id: reading the notification back would be a
// second query for what the publisher already had.
type EmailRequested struct {
	NotificationID uuid.UUID `json:"notificationId"`
	Recipient      uuid.UUID `json:"recipientId"`
	// To is the address as it was when the notice was raised. Resolving it in
	// the worker would send to whatever it had become by then.
	To    string    `json:"to"`
	Title string    `json:"title"`
	Body  string    `json:"body,omitempty"`
	Link  string    `json:"link,omitempty"`
	At    time.Time `json:"at"`
}

// Read is the payload of EventRead: the recipient has seen it.
type Read struct {
	NotificationID uuid.UUID `json:"notificationId"`
	Recipient      uuid.UUID `json:"recipientId"`
	At             time.Time `json:"at"`
}
