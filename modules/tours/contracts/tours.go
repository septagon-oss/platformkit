// Package contracts names the tenant-scoped tour runtime shared by callers.
package contracts

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
)

const (
	PermissionRead  = "tours:read"
	PermissionRun   = "tours:run"
	EventStarted    = "tours.started"
	EventStepViewed = "tours.step_viewed"
	EventCompleted  = "tours.completed"
	EventDismissed  = "tours.dismissed"
)

// Run is one immutable-edition playback session.
type Run struct {
	entity.Base
	App             string     `json:"-" gorm:"not null"`
	PersonID        uuid.UUID  `json:"-" gorm:"type:uuid;not null"`
	TourID          string     `json:"tourId" gorm:"not null"`
	Edition         uint32     `json:"edition" gorm:"not null"`
	ContentRevision string     `json:"contentRevision" gorm:"not null"`
	ContractHash    string     `json:"contractHash" gorm:"not null"`
	StepIDs         []string   `json:"-" gorm:"serializer:json;type:jsonb;not null"`
	Source          string     `json:"source" gorm:"not null"`
	Status          string     `json:"status" gorm:"not null"`
	CurrentStep     string     `json:"currentStep,omitempty" gorm:"not null"`
	Done            []string   `json:"done" gorm:"serializer:json;type:jsonb;not null"`
	Skipped         []string   `json:"skipped" gorm:"serializer:json;type:jsonb;not null"`
	Revision        uint64     `json:"revision" gorm:"not null"`
	LaterUntil      *time.Time `json:"laterUntil,omitempty"`
	StartedAt       time.Time  `json:"startedAt"`
	ViewedAt        *time.Time `json:"viewedAt,omitempty"`
	CompletedAt     *time.Time `json:"completedAt,omitempty"`
	DismissedAt     *time.Time `json:"dismissedAt,omitempty"`
}

// Event is a tenant-safe summary of a run transition.
type Event struct {
	RunID    uuid.UUID `json:"runId"`
	TourID   string    `json:"tourId"`
	Edition  uint32    `json:"edition"`
	StepID   string    `json:"stepId,omitempty"`
	Source   string    `json:"source"`
	PersonID uuid.UUID `json:"personId"`
	At       time.Time `json:"at"`
}

func (*Run) TableName() string { return "tours_runs" }

// Start identifies a run pinned to a composed tour artifact.
type Start struct {
	TourID       string
	Edition      uint32
	StepIDs      []string
	ContractHash string
	Source       string
}

// Action is one revision-checked playback command.
type Action struct {
	Kind             string
	Step             string
	ExpectedRevision uint64
	At               time.Time
}

// Clock supplies UTC instants for persisted tour facts.
type Clock func() time.Time

// Service starts, reads, and changes tour runs inside a tenant transaction.
type Service interface {
	Start(context.Context, db.Tx[db.Tenant], Start) (*Run, error)
	Get(context.Context, db.Tx[db.Tenant], uuid.UUID, uuid.UUID) (*Run, error)
	Act(context.Context, db.Tx[db.Tenant], uuid.UUID, uuid.UUID, Action) (*Run, error)
}
