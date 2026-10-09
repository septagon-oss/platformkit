package screens

import (
	"github.com/septagon-oss/platformkit/kit/entity"
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
// Hence a pointer per key. `Singular: ""` spelled out by an author and silence
// are two different documents; with value types they would be one.

// EntryPresentation is one resource as its author said it reads.
type EntryPresentation struct {
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
// default, reads no schema and knows nothing about what an icon is.
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
func deviations(p entity.EntryHints) *EntryPresentation {
	out := EntryPresentation{
		Singular: declared(p.Singular), Plural: declared(p.Plural),
		Description: declared(p.Description), Icon: declared(p.Icon),
		Group: p.Group, Order: declared(p.Order),
		PrimaryField: declared(p.PrimaryField), PreviewField: declared(p.PreviewField),
		StatusField: declared(p.StatusField), EmptyDescription: declared(p.EmptyDescription),
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
