// Package resource renders resolved entity policies and supplied rows as HTML.
// It performs no I/O; ui/screens supplies authorized routes and request values.
// See README.md for composition, fallback and value-reading contracts.
package resource

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"time"
	"unicode/utf8"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/ui/components"
)

// Resource supplies a schema, immutable resolved policy and authorized routes.
// When Resolved is absent or unsupported, the original declarations resolve once.
type Resource struct {
	// Resolved is an immutable policy for this render; unsupported versions resolve the original declarations.
	Resolved *present.Resolved
	// CommandsOnly suppresses entity surfaces when resolving a legacy command projection.
	CommandsOnly bool
	// ScreenOwner enables composed screen references; empty keeps standalone rendering.
	ScreenOwner string
	Schema      entity.Schema
	Immutable   []string
	// API is the registered JSON read address, or empty for offline rendering.
	// It scopes rich-text drafts without inferring a route from the screen address.
	API string
	// Present supplies legacy declarations used only when Resolved cannot be read.
	Present entity.EntryHints
	// Screen is the registered collection or singleton page address.
	// Offline callers supply their own address; all navigation starts here.
	Screen string
	// Singleton identifies an id-less record surface with no collection or create/delete doors.
	Singleton bool
	// Operations lists mounted CRUD verbs. Nil means all five; an explicit empty
	// set means none. These route capabilities narrow the resolved presentation.
	Operations []string
	// Commands contains only callable routes authorized for this render.
	// The policy supplies their words and flags; a policy alone grants no route.
	Commands []Command
	// History contains the versions already read under the record's guard.
	// Nil hides the panel; Carries avoids duplicating its restore command.
	History *HistoryPanel
	// Selected holds the equality selections this page is showing, field to
	// value, each one already validated and applied to the read by the caller.
	// The list's sort, facet and pager links carry them, so turning the page or
	// changing the order keeps the filter a person came to narrow. What a page
	// may offer stays in Resolved.List.Filters: this names what was chosen, not
	// what is offered, and ui/screens fills it in from the request's query.
	// A render only reads it.
	Selected map[string]string
	// Saving is this caller's standing against the row being drawn. It is read
	// by ui/screens in the request transaction; nil draws no saving control.
	Saving *SavingControl
}

// SavingControl carries the caller's read and the mounted addresses for saving
// one row. Commands recheck its grants and revision in their own transaction.
type SavingControl struct {
	Pressed   bool
	CanSave   bool
	CanRemove bool
	SaveURL   string
	UnsaveURL string
	ListURL   string
	ItemID    string
	Revision  int64
}

// SavingReader answers the request's saving question for one declared row.
type SavingReader interface {
	SavingState(ctx context.Context, module, kind, key string) (*SavingControl, error)
}

// PerPage is a screenful, and the page the list renders and links. kit/crud's
// default limit is the same number, so a page of a list and a page of the
// collection route are the same page; ui/screens, which imports both, checks
// that they agree.
const PerPage = 50

// Options is what a shell decides about its generated screens.
type Options struct {
	// Location is the viewer zone; nil renders UTC before browser enhancement.
	Location *time.Location
	// Workspace is the address the breadcrumb's first entry links to: the
	// workspace's own root. It is not a prefix the renderer composes paths
	// from — a screen is mounted at an address the kernel derived, and the
	// renderer is handed it. There used to be a Root here, and every module's
	// nav entry derived its link from it, which is how a shell's location
	// became nine modules' business.
	Workspace string
	// Home is the breadcrumb's first entry's label, linking to Workspace.
	Home string
	// PictureSource substitutes {id} in an authorized picture delivery URL.
	// Empty means this installation offers no stored-picture delivery door.
	PictureSource string
	// ComposedSource substitutes {reference} with a validated composed-picture token.
	ComposedSource string
	// Locale is the request's selected language when the shell composes
	// page.Shell.Messages; the adapter sets it per request. The fixed labels
	// — New, Edit, Delete, the count, the pager and the empty state — are
	// looked up under screens.* keys with their English as the fallback, the
	// same seam the sign-in page uses. Nil keeps the English. Entity names
	// stay as the schema humanizes them.
	Locale *locale.Locale
	// Refusal is the verdict a refused write came back with, worded by the layer that
	// renders refusals (ui/page) rather than by the code that caught the error. The
	// render that redraws a form after a refusal shows its heading and tone from here
	// and keeps the sentence it was handed as `detail`; nil is every render that names
	// no verdict — a design export, a shell drawing the writer's own line — and draws
	// what it drew before this field existed.
	Refusal *Refusal
	// Rendered is the instant this render measures relative values against,
	// sampled once by whoever drew the request. Its zero value names no instant,
	// and a relative value then keeps the absolute text an offline render or a
	// design export has always drawn. The instant arrives as a value rather than
	// a function to call because Options is a frozen comparable surface: a func
	// or map field here would stop `a == b` from compiling for whoever holds
	// two. It also says who owns the sample: the request takes it once, so no
	// two values on one page are measured against two different nows.
	Rendered time.Time
	// Saving reads the caller's standing for a row through the mounted capability.
	Saving SavingReader
}

// Refusal is one verdict as a form shows it: what the refusal page calls it, and how
// loudly it is announced. Both arrive worded and classified by whoever owns refusals;
// this package renders them and decides neither.
type Refusal struct {
	Heading string
	Tone    string
}

// Text is one label through the seam: the catalog's translation of key, or
// fallback formatted with args when no catalog is composed or lacks the key.
// The adapter reads it for the titles its mounted routes name.
func (o Options) Text(key, fallback string, args ...any) string {
	if o.Locale == nil {
		return fmt.Sprintf(fallback, args...)
	}
	return o.Locale.Text(key, fallback, args...)
}

// chromeFormatter is a render's locale as a form's seam for its chrome. A nil
// *locale.Locale sitting in a locale.Formatter interface is not nil to the code
// that asks it, so a render that composed no locale composes no formatter and
// answers its chrome with source text instead of dying on the way there.
func chromeFormatter(selected *locale.Locale) locale.Formatter {
	if selected == nil {
		return nil
	}
	return selected
}

// deleteForm is the destructive action: a real form, so it works without
// JavaScript, carrying the attribute confirm.js opens the dialog on. anchor names
// the record's published view, empty for a standalone render.
func deleteForm(o Options, item, entity string, anchor map[string]string) g.Node {
	remove := o.Text("screens.delete", "Delete")
	attrs := map[string]string{"data-confirm": o.Text("screens.delete_confirm", "This deletes the %s. It cannot be undone.", entity), "data-confirm-label": remove}
	maps.Copy(attrs, anchor)
	return components.Form(components.FormProps{Action: item + "/delete", Label: o.Text("screens.delete_this", "Delete this %s", entity)},
		components.Button(components.ButtonProps{
			ComponentProps: components.ComponentProps{Attrs: attrs},
			Label:          remove, Type: "submit", Tone: "danger", Size: "md",
		}))
}

func breadcrumb(o Options, collection, at, here string) g.Node {
	return components.Breadcrumb(components.BreadcrumbProps{
		NavigationLabel: o.Text("screens.breadcrumb", "Breadcrumb"),
		Items: []components.BreadcrumbItem{
			{Label: o.Home, Href: o.Workspace}, {Label: collection, Href: at}, {Label: here},
		}})
}

// collectionName is the resource's plural as a label rather than as a noun in a
// sentence: the word the policy holds, exactly as its author wrote it, with the
// letter a reader starts a line with. A list's heading, the name of its table
// region and a breadcrumb's collection entry each stand alone, so each begins a
// sentence; the count keeps the bare noun, because "3 notes" sits inside a
// sentence and starts none. Nothing is re-spelled here: no pluralising, no
// re-segmenting of a word an author chose — the first letter only.
func (r Resource) collectionName() string { return startSentence(r.Resolved.Identity.Plural) }

// startSentence puts the page's own capital on the first letter of a word that
// begins a line. The rest of the word is the author's, letter for letter.
func startSentence(word string) string {
	first, size := utf8.DecodeRuneInString(word)
	if first == utf8.RuneError && size <= 1 {
		return word
	}
	return strings.ToUpper(string(first)) + word[size:]
}

// offers reports whether this resource mounted the route for a verb. A nil
// Operations means all five and an empty one that none is mounted, the pair
// present reads; the question itself is stated once on httpx.Resource, which
// decides the route table, and this is its answer at the door.
func (r Resource) offers(verb string) bool {
	if r.Operations == nil {
		return true
	}
	for _, o := range r.Operations {
		if o == verb {
			return true
		}
	}
	return false
}

// recordAddress is the address a row's identity column links to, and it is empty
// for a resource that mounted no record route: a list of rows nobody may open is
// drawn as rows, not as links into the 404 of a place nothing is served. The page
// side and the door side already asked Offers read; the table's own link is the
// last place that asks it.
func (r Resource) recordAddress() string {
	if !r.offers("read") {
		return ""
	}
	return r.Screen
}
