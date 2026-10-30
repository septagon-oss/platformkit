package contracts

import (
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/events"
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

// Events is the compatibility declaration list for consumers that need the task event set.
var Events = []events.Declared{
	TopicCreated.Declaration(), TopicUpdated.Declaration(), TopicDeleted.Declaration(),
	TopicAssigned.Declaration(), TopicResolved.Declaration(), TopicSLABreached.Declaration(),
}

var (
	TopicCreated     = events.DeclareTopic[*Task](EventCreated)
	TopicUpdated     = events.DeclareTopic[*Task](EventUpdated)
	TopicDeleted     = events.DeclareTopic[*Task](EventDeleted)
	TopicAssigned    = events.DeclareTopic[Assigned](EventAssigned)
	TopicResolved    = events.DeclareTopic[Resolved](EventResolved)
	TopicSLABreached = events.DeclareTopic[SLABreached](EventSLABreached)
)

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
