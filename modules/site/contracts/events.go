package contracts

import (
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/events/transport"
)

// The one event this module emits. There is no created and no deleted: the
// settings of a tenant are not created — every tenant has some from the moment
// it exists — and they are not deleted either, because a site without settings
// is a site that cannot render.
const EventSettingsUpdated = "site.settings_updated"

// Events is every event this module emits, for the manifest.
var Events = []string{EventSettingsUpdated}

// Payloads is the type of that event's payload, for the manifest and the
// documents the composition projects from it. rest.Singleton names the event;
// what the save puts in it is SettingsUpdated, the values a cache keys on,
// rather than the whole settings row.
var Payloads = []transport.Declared{
	transport.Declare[SettingsUpdated](EventSettingsUpdated),
}

// SettingsUpdated is the payload: what the site is now. It carries the values a
// cache would key on rather than only an id, because the subscriber this exists
// for is whatever renders the public site, and it should not have to read the
// row back to know the title changed.
type SettingsUpdated struct {
	SettingsID uuid.UUID `json:"settingsId"`
	Title      string    `json:"title,omitempty"`
	HomeSlug   string    `json:"homeSlug,omitempty"`
	Theme      string    `json:"theme"`
	At         time.Time `json:"at"`
}
