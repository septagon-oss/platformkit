package contracts

import (
	"time"

	"github.com/google/uuid"
)

// The one event this module emits. There is no created and no deleted: the
// settings of a tenant are not created — every tenant has some from the moment
// it exists — and they are not deleted either, because a site without settings
// is a site that cannot render.
const EventSettingsUpdated = "site.settings_updated"

// Events is every event this module emits, for the manifest.
var Events = []string{EventSettingsUpdated}

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
