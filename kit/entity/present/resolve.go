package present

import (
	"cmp"
	"slices"
	"strings"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/display"
)

// Input is everything Resolve decides from: one registration as the catalogue
// holds it for one request. Resolve reads it and writes none of it, so a shared
// registration may be resolved concurrently in several languages.
type Input struct {
	// Schema and Hints are the registration's, with declared words already
	// translated by the caller.
	Schema entity.Schema
	Hints  entity.EntryHints
	// Immutable names the fields a command owns, read-only in an update form.
	Immutable []string
	// Singleton resolves no list: a tenant has one record, at the path itself.
	Singleton bool
	// CommandsOnly resolves no fields, layout or list, only the commands.
	CommandsOnly bool
	// Operations are the routes the resource offers; nil is all five, empty is none.
	Operations []string
	// Commands are the commands the caller may call, in order.
	Commands []CommandInput
	// Language is the tag the caller translated for; "" is English.
	Language string
	// Text translates the words this package introduces (see the present.* keys);
	// nil keeps the English literal.
	Text func(key, fallback string) string
}

// CommandInput is one command the caller may call.
type CommandInput struct {
	Fields     []entity.Field
	Bindings   map[string]string
	Collection bool
	Repeated   bool
	Verb       string
	Summary    string
	Undo       string
	HasResult  bool
	Hints      entity.CommandHints
}

// Resolve decides the presentation of one entry. It never fails: a hint that
// names a field the schema lacks, or a word outside the vocabulary, resolves to
// the neutral reading, because the mount gate is where such a hint is refused.
func Resolve(in Input) Resolved {
	r := Resolved{
		PolicyVersion: PolicyVersion,
		Language:      cmp.Or(in.Language, "en"),
		Identity:      identity(in),
		Fields:        []ResolvedField{},
		Commands:      commands(in),
		Deck:          Deck(in.Hints.Deck, in.Commands),
		Summary:       ResolvedSummary{Highlight: in.Hints.Highlight, TrendField: in.Hints.TrendField},
	}
	if in.CommandsOnly {
		r.Titles = ResolvedTitles{EmptyTitle: in.word("present.emptyTitle", "—"), ListCandidates: []string{}, RecordCandidates: []string{}, Summary: []string{}}
		r.Layout = emptyLayout()
		r.Fields = []ResolvedField{}
		r.List = ResolvedList{Sorts: []ResolvedSortOption{}, Filters: []ResolvedFilter{}}
		r.Views = views(in, r.Layout, r.Fields)
		return r
	}
	r.Titles = titles(in)
	r.Layout = layout(in, r.Titles)
	r.Fields = fields(in)
	r.List = list(in)
	r.Views = views(in, r.Layout, r.Fields)
	return r
}

func (in Input) word(key, fallback string) string {
	if in.Text == nil {
		return fallback
	}
	return in.Text(key, fallback)
}

func (in Input) offers(op string) bool {
	if in.Singleton && op != "update" && op != "read" {
		return false
	}
	return in.Operations == nil || slices.Contains(in.Operations, op)
}

func identity(in Input) ResolvedIdentity {
	h := in.Hints
	singular := cmp.Or(h.Singular, strings.ToLower(display.Humanize(in.Schema.Entity)))
	out := ResolvedIdentity{
		Singular: singular, Plural: cmp.Or(h.Plural, display.Plural(singular)),
		Description: h.Description, EmptyDescription: h.EmptyDescription,
		Order: h.Order,
	}
	if entity.ValidIcon(h.Icon) {
		out.Icon = h.Icon
	}
	if h.Group != nil {
		out.Group = &ResolvedGroup{Key: h.Group.Key, Label: h.Group.Label}
	}
	return out
}

func commands(in Input) []ResolvedCommand {
	out := make([]ResolvedCommand, 0, len(in.Commands))
	for _, c := range in.Commands {
		h := c.Hints
		rc := ResolvedCommand{
			Fields: commandFields(in, c),
			Verb:   c.Verb, Label: cmp.Or(h.Label, c.Summary, display.Humanize(c.Verb)),
			Visible: !h.System, System: h.System, Primary: h.Primary, Destructive: h.Destructive,
			SuccessMessage: h.SuccessMessage,
		}
		if h.Confirmation != nil {
			rc.Confirmation = &ResolvedConfirmation{Title: h.Confirmation.Title, Body: h.Confirmation.Body, ConfirmLabel: h.Confirmation.ConfirmLabel}
		}
		out = append(out, rc)
	}
	return out
}

// Deck is the swipe deck narrowed to the commands the caller may run, nil when
// the entry declares none. A direction whose command is not callable is left
// out; a deck that cannot undo every direction it kept is not undoable.
func Deck(d *entity.DeckHints, commands []CommandInput) *ResolvedDeck {
	if d == nil {
		return nil
	}
	d = d.Clone()
	out := &ResolvedDeck{Queue: slices.Clone(d.Queue), Order: d.Order, Directions: map[string]ResolvedDeckDirection{}, Undoable: d.Undoable}
	allowed := map[string]bool{}
	byName := map[string]CommandInput{}
	for _, c := range commands {
		byName[c.Verb] = c
		if !c.Hints.System {
			allowed[c.Verb] = true
		}
	}
	for key, v := range d.Directions {
		if v.Skip {
			out.Directions[key] = ResolvedDeckDirection{Skip: true}
			continue
		}
		if !allowed[v.Command] {
			continue
		}
		direction := ResolvedDeckDirection{Command: v.Command, Label: v.Label, Tone: v.Tone, Icon: v.Icon}
		if c := byName[v.Command]; c.Undo != "" && allowed[c.Undo] && c.HasResult {
			direction.Undo = c.Undo
		} else {
			out.Undoable = false
		}
		out.Directions[key] = direction
	}
	if out.Queue == nil {
		out.Queue = []string{}
	}
	if len(out.Directions) == 0 {
		out.Undoable = false
	}
	return out
}
