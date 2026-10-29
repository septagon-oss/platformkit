package screens

import (
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

// hints.go is the entry- and command-level reading contract as the catalogue
// serves it, and the one place the rule behind it is written: a key is on the
// wire because its author put it there, and never because the server had an
// opinion about what it would have been.
//
// That rule is `derived` and `write_path` in catalog.go, applied to a whole
// object: an entry nobody hinted carries no `presentation` key at all, so every
// document every shell already parsed keeps its bytes and CatalogVersion stays
// where it is. The other half is just as fixed — a declared value is echoed even
// when it is what a conforming consumer would have computed anyway, because
// suppressing it would put the server's opinion about the default *into* the
// document and make "did somebody choose this or did nobody say anything?"
// unanswerable from the bytes.
//
// Hence a pointer per key: the distinction the document can carry is the one
// between a key with a value in it and no key at all, and a value type would
// print a default for an author who said nothing.
//
// What no Go type can carry is a declared empty value beside silence: the mount
// gate refuses the declarations whose empty form means nothing (`group` with no
// label, `sections` with no block), and an empty `singular` is silence by
// definition — there is no word a person was going to read that is the empty
// string. `declared` is therefore "this was said" for every non-zero value and
// never for the zero one, which is the whole of what it promises.

// EntryPresentation is one resource as its author said it reads.
type EntryPresentation struct {
	Deck             *DeckPresentation       `json:"deck,omitempty"`
	Singular         *string                 `json:"singular,omitempty"`
	Plural           *string                 `json:"plural,omitempty"`
	Description      *string                 `json:"description,omitempty"`
	Icon             *string                 `json:"icon,omitempty"`
	Group            *entity.ResourceGroup   `json:"group,omitempty"`
	Order            *int                    `json:"order,omitempty"`
	PrimaryField     *string                 `json:"primaryField,omitempty"`
	PreviewField     *string                 `json:"previewField,omitempty"`
	SummaryFields    *[]string               `json:"summaryFields,omitempty"`
	StatusField      *string                 `json:"statusField,omitempty"`
	Sections         *[]entity.EntitySection `json:"sections,omitempty"`
	EmptyDescription *string                 `json:"emptyDescription,omitempty"`
	Sortable         *[]string               `json:"sortable,omitempty"`
	CoverField       *string                 `json:"coverField,omitempty"`
	ThumbnailField   *string                 `json:"thumbnailField,omitempty"`
	PrimaryCommand   *string                 `json:"primaryCommand,omitempty"`
	// Highlight is the declared sub-count — the words and the predicate, not the
	// number. The answer is in /summary, which carries no words at all, so the
	// label lives here where every other declared string is resolved for language.
	Highlight *entity.EntryHighlight `json:"highlight,omitempty"`
	// TrendField names the timestamp a shell may break into days. It names a
	// field and never a window: the fourteen days are the answer's, not a
	// declaration's.
	TrendField *string `json:"trendField,omitempty"`
}

// CommandPresentation is one lifecycle route as its author described it.
type CommandPresentation struct {
	Label          *string                     `json:"label,omitempty"`
	Primary        *bool                       `json:"primary,omitempty"`
	Destructive    *bool                       `json:"destructive,omitempty"`
	System         *bool                       `json:"system,omitempty"`
	Confirmation   *entity.CommandConfirmation `json:"confirmation,omitempty"`
	SuccessMessage *string                     `json:"successMessage,omitempty"`
}

// declared is the whole rule in one shape: a zero field of the declaration
// prints no key, a non-zero one prints the author's value. It computes no
// default, reads no schema and knows nothing about what an icon is. The zero
// value prints nothing for every type it reads — "no opinion" and "nobody said
// anything" are the same document, and `Order` is the case that says so in its
// own comment in kit/entity.
func declared[T comparable](value T) *T {
	var none T
	if value == none {
		return nil
	}
	return &value
}

// deviations is an entry's declaration as the document writes it, and nil for
// the zero EntryHints — which is what "nobody said anything" looks like on the
// wire, and the reason an un-hinted entry's bytes do not move.
func deviations(p entity.EntryHints, commands []httpx.Command) *EntryPresentation {
	out := EntryPresentation{
		Deck:     deckPresentation(p.Deck, commands),
		Singular: declared(p.Singular), Plural: declared(p.Plural),
		Description: declared(p.Description), Icon: declared(p.Icon),
		Group: p.Group, Order: declared(p.Order),
		PrimaryField: declared(p.PrimaryField), PreviewField: declared(p.PreviewField),
		StatusField: declared(p.StatusField), EmptyDescription: declared(p.EmptyDescription),
		CoverField: declared(p.CoverField), ThumbnailField: declared(p.ThumbnailField),
		PrimaryCommand: declared(p.PrimaryCommand), Highlight: p.Highlight,
		TrendField: declared(p.TrendField),
	}
	if p.SummaryFields != nil {
		out.SummaryFields = &p.SummaryFields
	}
	if p.Sections != nil {
		out.Sections = &p.Sections
	}
	if p.Sortable != nil {
		out.Sortable = &p.Sortable
	}
	if out == (EntryPresentation{}) {
		return nil
	}
	return &out
}

// commandDeviations is a command's declaration, on the same rule.
func commandDeviations(p entity.CommandHints) *CommandPresentation {
	out := CommandPresentation{
		Label: declared(p.Label), Primary: declared(p.Primary),
		Destructive: declared(p.Destructive), System: declared(p.System),
		Confirmation: p.Confirmation, SuccessMessage: declared(p.SuccessMessage),
	}
	if out == (CommandPresentation{}) {
		return nil
	}
	return &out
}

// DeckPresentation is a list reading with caller-authorized gesture commands.
// Directions remains an object when the caller may run none of the commands.
type DeckPresentation struct {
	Undoable   bool                                 `json:"undoable,omitzero"`
	Queue      []string                             `json:"queue,omitempty"`
	Order      string                               `json:"order"`
	Directions map[string]DeckDirectionPresentation `json:"directions"`
}

// DeckDirectionPresentation carries either command copy or a down-only skip.
// Undo names the authorized command which accepts the returned private token.
type DeckDirectionPresentation struct {
	Undo    string `json:"undo,omitempty"`
	Command string `json:"command,omitempty"`
	Label   string `json:"label,omitempty"`
	Tone    string `json:"tone,omitempty"`
	Icon    string `json:"icon,omitempty"`
	Skip    bool   `json:"skip,omitzero"`
}

// deckPresentation is the deck's wire form: present.Deck decides which
// directions the caller may run and whether the deck can undo them.
func deckPresentation(d *entity.DeckHints, commands []httpx.Command) *DeckPresentation {
	inputs := make([]present.CommandInput, 0, len(commands))
	for _, c := range commands {
		inputs = append(inputs, present.CommandInput{Verb: c.Verb, Undo: c.Undo, HasResult: c.HasResult, Hints: c.Present})
	}
	deck := present.Deck(d, inputs)
	if deck == nil {
		return nil
	}
	out := &DeckPresentation{Queue: deck.Queue, Order: deck.Order, Undoable: deck.Undoable, Directions: map[string]DeckDirectionPresentation{}}
	if len(out.Queue) == 0 {
		out.Queue = nil
	}
	for key, v := range deck.Directions {
		out.Directions[key] = DeckDirectionPresentation{Undo: v.Undo, Command: v.Command, Label: v.Label, Tone: v.Tone, Icon: v.Icon, Skip: v.Skip}
	}
	return out
}
