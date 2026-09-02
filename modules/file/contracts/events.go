package contracts

import (
	"time"

	"github.com/google/uuid"
)

// The two events this module emits. There is no rest.Spec here, so there is no
// file.file.created either: a file arrives as bytes and leaves as bytes, and
// the two routes that do it publish these.
const (
	EventUploaded = "file.uploaded"
	EventDeleted  = "file.deleted"
)

// Events is every event this module emits, for the manifest.
var Events = []string{EventUploaded, EventDeleted}

// Uploaded is the payload of EventUploaded. It carries the digest as well as
// the size because the subscriber this exists for is whatever indexes or scans
// an upload, and both are things it would otherwise read the row back for.
type Uploaded struct {
	FileID      uuid.UUID `json:"fileId"`
	Name        string    `json:"name"`
	ContentType string    `json:"contentType"`
	Size        int64     `json:"size"`
	SHA256      string    `json:"sha256"`
	Visibility  string    `json:"visibility"`
	At          time.Time `json:"at"`
}

// Deleted is the payload of EventDeleted, and it carries the storage key
// because by the time anybody handles it the row is gone. That is the whole
// reason this event exists: removing the bytes is work that has to happen after
// the transaction that removed the row commits, and an event is the only thing
// in this architecture that is delivered exactly then.
type Deleted struct {
	FileID     uuid.UUID `json:"fileId"`
	StorageKey string    `json:"storageKey"`
	Size       int64     `json:"size"`
	At         time.Time `json:"at"`
}
