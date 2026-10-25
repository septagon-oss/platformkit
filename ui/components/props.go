package components

// Props are the typed inputs to the component constructors in this package.
// Example captures those same inputs and derives portable property schemas;
// screens.FormExample uses that path for generated entity forms, and export.Export
// carries the captured contracts to design consumers. See example.go.
//
// These contracts describe presentation. A2UI/MCP transport and native renderers
// require their own adapters; neither protocol processing nor entity persistence
// is implemented by a Props schema.

// ComponentProps is the base set of properties shared by all components.
type ComponentProps struct {
	// ID is a DOM identity supplied by trusted Go composition. Portable property
	// schemas omit it; captured source occurrences use ExampleInfo.ID separately.
	ID string `json:"id,omitempty" delivery:"internal"`
	// Class is a renderer escape hatch for trusted Go composition. It is not a
	// portable design-system property and is therefore excluded from delivery.
	Class    string `json:"class,omitempty" delivery:"internal"`
	Disabled bool   `json:"disabled,omitempty"`
	Hidden   bool   `json:"hidden,omitempty"`
	// Attrs is restricted to trusted direct-render callers; portable contracts
	// expose explicit typed properties instead of arbitrary HTML attributes.
	Attrs map[string]string `json:"attrs,omitempty" delivery:"internal"`
}

// HTMXProps contains HTMX-specific properties for server-driven interactions.
type HTMXProps struct {
	Get         string `json:"hx-get,omitempty"`
	Post        string `json:"hx-post,omitempty"`
	Put         string `json:"hx-put,omitempty"`
	Patch       string `json:"hx-patch,omitempty"`
	Delete      string `json:"hx-delete,omitempty"`
	Target      string `json:"hx-target,omitempty"`
	Swap        string `json:"hx-swap,omitempty"`
	Trigger     string `json:"hx-trigger,omitempty"`
	Include     string `json:"hx-include,omitempty"`
	Confirm     string `json:"hx-confirm,omitempty"`
	Ext         string `json:"hx-ext,omitempty"`
	Indicator   string `json:"hx-indicator,omitempty"`
	DisabledElt string `json:"hx-disabled-elt,omitempty"`
	Vals        string `json:"hx-vals,omitempty"`
	PushURL     string `json:"hx-push-url,omitempty"`
	Select      string `json:"hx-select,omitempty"`
	Boost       bool   `json:"hx-boost,omitempty"`
	Disable     bool   `json:"hx-disable,omitempty"`
}

// SkeletonProps lives in skeleton.go: the earlier
// contract-only draft here (free-string width/height) predated the audited
// class pipeline and had no renderer or consumers.

// ContentState describes an authorized read; it never infers connectivity or access.
type ContentState struct {
	Status       MediaStatus `json:"status,omitempty" enum:",ready,loading,empty,failed,refused"`
	Title        string      `json:"title,omitempty"`
	Text         string      `json:"text,omitempty"`
	LoadingLabel string      `json:"loadingLabel,omitempty"`
	Offline      bool        `json:"offline,omitzero"`
	OfflineText  string      `json:"offlineText,omitempty"`
}

// ChoiceLink is caller-owned navigation; a key never becomes a URL.
type ChoiceLink struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Href        string `json:"href,omitempty"`
	Selected    bool   `json:"selected,omitzero"`
	Disabled    bool   `json:"disabled,omitzero"`
	RemoveHref  string `json:"removeHref,omitempty"`
	RemoveLabel string `json:"removeLabel,omitempty"`
}

// MediaStatus is where a picture has got to. It exists because a picture that
// has not arrived yet, a picture that never will, and a picture somebody is not
// allowed to see all look the same — a broken-image glyph — to the only renderer
// this repository had before: Card emitted an <img> whatever it knew.
//
// The five names are the shared view-state vocabulary, not a media invention:
// the same words carry a list, a trail and a cut-out, so a screen does not say
// "no results" about a refusal and "error" about an empty shelf. A capability
// that owns a different lifecycle maps onto these five at its own edge and says
// which words it used; nothing below this package invents a sixth.
type MediaStatus string

const (
	// MediaReady renders the picture. An empty Status means ready, because
	// everything that has a source has one to show.
	MediaReady MediaStatus = "ready"
	// MediaLoading holds the box the picture will land in, and says so to a
	// screen reader rather than leaving it guessing at a rectangle.
	MediaLoading MediaStatus = "loading"
	// MediaEmpty is "there is nothing here", which is a fact about the shelf,
	// not about the viewer.
	MediaEmpty MediaStatus = "empty"
	// MediaFailed is "we could not get it", with the reason in the capability's
	// own words. It is not empty: an error that renders as an empty state loses
	// the one thing a caller needs to hear.
	MediaFailed MediaStatus = "failed"
	// MediaRefused is "you may not see this". Distinct from empty on purpose:
	// hiding a permission behind "nothing here" teaches everybody that the
	// product lies, and it is the difference between asking for access and
	// going to look somewhere else.
	MediaRefused MediaStatus = "refused"
)
