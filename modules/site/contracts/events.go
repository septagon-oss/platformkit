package contracts

import (
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/events"
)

// The one event this module emits. There is no created and no deleted: the
// settings of a tenant are not created — every tenant has some from the moment
// it exists — and they are not deleted either, because a site without settings
// is a site that cannot render.
const EventSettingsUpdated = "site.settings_updated"

// Events is every event this module emits, for the manifest.
var Events = []events.Declared{events.Declare[SettingsUpdated](EventSettingsUpdated)}

// SettingsUpdated is the payload: what the site is now, and what moved to get
// there. It carries the values a cache would key on rather than only an id,
// because the subscriber this exists for is whatever renders the public site, and it
// should not have to read the row back to know the title changed.
//
// Changes is the same save seen from the trail: the fields the save moved, under the
// names this payload uses for them, each with the value it left and the value it took
// (kit/events.Change; the diff is computed over the stored row, which Save reads FOR
// UPDATE so the before half is the value this save actually replaced). The trail
// records payloads verbatim and invents no before/after it was not given, so a change
// that wants to be explainable a year later carries its own explanation — which is the
// half of the core review of 2026-09-29 (P2) this module owns: two saves that replaced
// one tagline with another used to leave two events naming neither.
type SettingsUpdated struct {
	SettingsID uuid.UUID `json:"settingsId"`
	Title      string    `json:"title,omitempty"`
	HomeSlug   string    `json:"homeSlug,omitempty"`
	Theme      string    `json:"theme"`
	At         time.Time `json:"at"`
	// Changes is the diff, and it is additive on purpose: every existing subscriber
	// reads the four values above and none of them had to change.
	Changes []events.Change `json:"changes,omitempty"`
}
