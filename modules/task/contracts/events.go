package contracts

import (
	"time"

	"github.com/google/uuid"
)

// The six events this module emits. The first three are kit/rest's, published
// by the Spec module.go mounts; the last three are the lifecycle's, published by
// the commands. Both sets are listed in the manifest, and kit/app refuses to
// start if a route would publish one that is not.
//
// A subscriber names one of these constants rather than a string, so renaming
// an event is a compile error in every module that listens for it.
const (
	EventCreated = "task.task.created"
	EventUpdated = "task.task.updated"
	EventDeleted = "task.task.deleted"

	EventAssigned    = "task.assigned"
	EventResolved    = "task.resolved"
	EventSLABreached = "task.sla_breached"
)

// Events is every event this module emits, for the manifest.
var Events = []string{EventCreated, EventUpdated, EventDeleted, EventAssigned, EventResolved, EventSLABreached}

// Assigned is the payload of EventAssigned: somebody is now responsible.
type Assigned struct {
	TaskID   uuid.UUID `json:"taskId"`
	Assignee uuid.UUID `json:"assigneeId"`
	// Status is what the assignment moved the task to, so a subscriber that
	// only wants acknowledgements does not have to read the task back.
	Status string    `json:"status"`
	At     time.Time `json:"at"`
}

// Resolved is the payload of EventResolved: the loop is closed.
type Resolved struct {
	TaskID     uuid.UUID `json:"taskId"`
	Resolution string    `json:"resolution,omitempty"`
	At         time.Time `json:"at"`
}

// SLABreached is the payload of EventSLABreached: the promise was broken. It
// carries the priority and the deadline because escalation is decided from
// those two and a subscriber should not need a second query to escalate.
type SLABreached struct {
	TaskID   uuid.UUID `json:"taskId"`
	Priority string    `json:"priority"`
	Deadline time.Time `json:"deadline"`
	At       time.Time `json:"at"`
}
