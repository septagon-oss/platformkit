// Package entity describes tenant-owned data and derives its field metadata.
// It does not open a database, authorize an operation or persist a value.
// Embed Base and implement TableName to describe a tenant-owned entity, or use
// FieldsOf for a plain input struct that needs no storage identity.
package entity

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Base is embedded by every tenant-owned entity. The kernel sets TenantID from
// the transaction's scope; a module never assigns it, and never sees it in
// JSON either, because a tenant that a caller could send is a tenant a caller
// could change.
//
// The three fields a caller may read are marked read-only for OpenAPI and not
// required in a request body: the server sets all of them, so a create that had
// to send an id would be a create that could choose one.
type Base struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id" required:"false" readOnly:"true"`
	TenantID  uuid.UUID  `gorm:"type:uuid;not null" json:"-"`
	CreatedAt time.Time  `json:"createdAt" required:"false" readOnly:"true"`
	UpdatedAt time.Time  `json:"updatedAt" required:"false" readOnly:"true"`
	DeletedAt *time.Time `gorm:"index" json:"-"`
}

// base is how the storage adapter reaches the embedded fields of any entity. It is
// unexported, so the Entity interface is closed to types that embed Base: a
// struct cannot claim to be an entity without carrying the tenant column that
// makes it one.
func (b *Base) base() *Base { return b }

// Entity is a tenant-owned row: a table name and an embedded Base.
type Entity interface {
	TableName() string
	base() *Base
}

// Validator is the optional check an entity makes of itself before it is
// written. It takes a context and no transaction: storage adapters own database
// constraints and translate validation errors into their own error contract.
type Validator interface {
	Validate(ctx context.Context) error
}

// BaseOf returns the embedded identity and tenancy fields of a non-nil entity.
// Storage adapters use it to stamp a value from their transaction's tenant;
// it does not authorize access or establish a transaction by itself.
func BaseOf(e Entity) *Base { return e.base() }
