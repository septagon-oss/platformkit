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
// Fallback is what one translatable field of one response really is: which
// language the value shown is written in, and how far behind the source it is.
//
// It exists because "the response is in Portuguese" is false of a response
// with a fallback in it, and a reader deserves to know which half is English.
// The closed set of Status is kit/rest's five: missing, outdated, machine,
// withheld and removed.
type Fallback struct {
	// Locale is the language the value in the field is actually written in —
	// the tenant's default for a fallback, the requested tag for an outdated or
	// machine translation the caller is being shown.
	Locale string `json:"locale"`
	Status string `json:"status"`
}

// LocaleState is one language's standing on one record: how many of its
// translatable fields the record is reviewed in, out of how many it has.
//
// It carries the two numbers rather than a percentage because they answer
// different questions: "8 of 9" names the field a translator still owes, and any
// rounding of it names nothing. A record is authored in the tenant's own language,
// so that language is never behind and is never answered here at all — a badge
// saying the source is 100% translated is a badge that can never be wrong, which is
// the same thing as a badge that says nothing.
type LocaleState struct {
	// Locale is the tag as this tenant declared it.
	Locale string `json:"locale"`
	// Reviewed counts the translatable fields with a translation a reader would
	// be served in this language: reviewed, and measured against a source that
	// still matches. A machine draft nobody signed off is not one.
	Reviewed int `json:"reviewed"`
	// Fields is how many translatable fields the record has at all.
	Fields int `json:"fields"`
}

// Percent is the reviewed share of this record in this language — the number a
// completeness badge wears. A record with no translatable field is complete in
// every language by there being nothing to translate, which is the one case where
// that answer is not a lie.
func (l LocaleState) Percent() int {
	if l.Fields == 0 {
		return 100
	}
	return l.Reviewed * 100 / l.Fields
}

type Base struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id" required:"false" readOnly:"true"`
	TenantID  uuid.UUID  `gorm:"type:uuid;not null" json:"-"`
	CreatedAt time.Time  `json:"createdAt" required:"false" readOnly:"true"`
	UpdatedAt time.Time  `json:"updatedAt" required:"false" readOnly:"true"`
	DeletedAt *time.Time `gorm:"index" json:"-"`
	// I18N names the fields whose value is not in the language this response is
	// answered in. Only the read path fills it and no caller ever does: a PATCH
	// that names "_i18n" is the same refusal as one that names a field the
	// entity does not have, because derive admits no map type, so this member
	// reaches no screen, no filter, no sort and no merge.
	//
	// It lives on Base rather than beside an entity because an embedded struct's
	// fields are promoted, which puts the member inline in the object — the one
	// place it can live without forking Item and Page into translated twins or
	// changing any resource's response type. Three tags make that safe, and each
	// is a boundary rather than a style:
	//
	//   gorm:"-"    it is not a column, so no read or write of a row touches it;
	//   omitempty  every entity with nothing translatable keeps the member
	//              absent, which is what makes this delivery invisible to a
	//              resource that declares no translatable field;
	//   hidden     it is not in the OpenAPI document, because a member the
	//              caller may read and may never send is a member the request
	//              schema must not offer — and Base is embedded by every entity
	//              in the installation, so one leaked field here is a leaked
	//              field in every operation's body.
	//
	// It is a pointer so that Base stays comparable: a map field would make
	// every entity struct in the kernel incomparable, which kit/crud's own test
	// and any caller that compares two loaded rows would find out about at
	// compile time. nil is "nothing fell back", which is what omitempty says.
	I18N *map[string]Fallback `gorm:"-" json:"_i18n,omitempty" readOnly:"true" hidden:"true"`
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
