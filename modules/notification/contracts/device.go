package contracts

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/push"
)

// maxToken is the longest token this module stores, the ceiling of the CHECK in
// migrations/000058. It is a shape limit rather than a protocol one: a browser
// subscription is a URL and two short keys, a phone token is a few dozen
// characters, and what reaches this length is a paste of the wrong thing.
const maxToken = 8192

// Device is one thing a person can be told at: a browser subscription or a phone
// token, belonging to one person in one tenant.
//
// Token is opaque here in the same way it is opaque in kit/push: its shape is
// its adapter's, and a command that stored a token it could not carry would fail
// every later notice to that person, which is why registration asks the adapter
// rather than a pattern of its own.
//
// Nothing else is a column. No label, no operating system, no app version and no
// last-seen timestamp: a field nothing reads is not written, and the platform is
// the only fact about a device any command branches on — it is also the routing
// key, so the row carries exactly what the delivery reads.
type Device struct {
	crud.Base

	// RecipientID is the person the device belongs to. Nothing but RegisterDevice
	// writes it, and what it may write is the caller.
	RecipientID uuid.UUID `json:"recipientId" gorm:"type:uuid;not null" format:"uuid" readOnly:"true" doc:"The person this device belongs to"`
	// Platform is what speaks the token, and the CHECK of migrations/000058
	// spelled in Go: the same list kit/push routes by.
	Platform push.Platform `json:"platform" gorm:"type:text;not null" validate:"required" doc:"What speaks this token" example:"expo"`
	// Token is the provider's token or the serialised subscription.
	Token string `json:"token" gorm:"type:text;not null" validate:"required" maxLength:"8192" doc:"The provider's token or serialised subscription; opaque outside its adapter"`
}

// TableName pins the table, so the entity and migrations/000058 agree.
func (Device) TableName() string { return "notification_devices" }

// Validate is the entity's own check, run by kit/crud on every write whichever
// door it came through. It is syntax only: whether this token is one this
// installation could carry is the adapter's answer, asked by RegisterDevice.
func (d *Device) Validate(context.Context) error {
	d.Token = strings.TrimSpace(d.Token)
	switch {
	case d.RecipientID == uuid.Nil:
		return fmt.Errorf("a device belongs to somebody")
	case d.Token == "":
		return fmt.Errorf("a device is an address, and this one has none")
	case len(d.Token) > maxToken:
		return fmt.Errorf("a token of %d bytes is longer than any device hands out", len(d.Token))
	case strings.ContainsAny(d.Token, "\n\r\t"):
		return fmt.Errorf("a token with a line break in it was copied out of something else")
	}
	return d.Platform.Validate()
}

// DeviceInput is what a person hands in to register the device they are holding.
// There is no recipient field: the caller is the person the row names, and a
// body that could name somebody else would be a way to fill their phone.
type DeviceInput struct {
	Platform push.Platform
	Token    string
}

// DeviceService is what a person does with the devices they registered.
//
// It is its own interface rather than more methods on Service for the reason
// contracts.PreferenceService gives: a caller that raises notices should not have
// to hold a way to change where somebody is told, and the fake a consumer wires
// for Notify is then not also a fake for the device store.
//
// No route, no command and no argument here reaches another person's devices.
// The scope is that the caller is the person the row names — the same fact the
// two notification routes rest on — and every command rechecks it inside the
// transaction it writes in.
type DeviceService interface {
	// RegisterDevice writes the caller's device, keyed by tenant, platform and
	// token. The same token from the same person changes nothing and publishes
	// nothing; the same token presented by another person of the tenant becomes
	// theirs, because whoever holds the token is whoever can be pushed to, which
	// is what a sold phone means for its previous owner.
	RegisterDevice(ctx context.Context, tx db.Tx[db.Tenant], recipient uuid.UUID, in DeviceInput) (*Device, error)

	// ListDevices is the caller's own live devices, newest first.
	ListDevices(ctx context.Context, tx db.Tx[db.Tenant], recipient uuid.UUID, q crud.Query) ([]*Device, int64, error)

	// RemoveDevice deletes one of them. An id that is not the caller's live
	// device answers crud.ErrNotFound — the same answer an id that never existed
	// gets, which is what lets the route stay SignedIn.
	RemoveDevice(ctx context.Context, tx db.Tx[db.Tenant], id, recipient uuid.UUID) error
}
