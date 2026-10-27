package present

import (
	"cmp"
	"slices"

	"github.com/septagon-oss/platformkit/kit/entity"
)

// ResolvedChoice carries effective semantics; Options stays the one vocabulary.
type ResolvedChoice struct {
	Cardinality     string                `json:"cardinality"`
	Empty           entity.ChoiceEmpty    `json:"empty"`
	Maximum         *entity.ChoiceMaximum `json:"maximum,omitempty"`
	NoPreference    string                `json:"noPreference"`
	SearchThreshold int                   `json:"searchThreshold"`
	SearchEnabled   bool                  `json:"searchEnabled"`
	Groups          []entity.ChoiceGroup  `json:"groups"`
}

func resolveChoice(f entity.Field, r *ResolvedField) {
	if f.Choices == nil {
		return
	}
	c := f.Choices.Clone()
	threshold := cmp.Or(c.SearchThreshold, 12)
	r.Choice = &ResolvedChoice{Cardinality: c.Cardinality, Empty: c.Empty, Maximum: c.Maximum, NoPreference: c.NoPreference, SearchThreshold: threshold, SearchEnabled: len(c.Options) > threshold, Groups: []entity.ChoiceGroup{}}
	r.Options = make([]ResolvedOption, 0, len(c.Options))
	for _, o := range c.DisplayOptions() {
		group := ""
		if o.Group != nil {
			group = o.Group.Key
			if !slices.ContainsFunc(r.Choice.Groups, func(g entity.ChoiceGroup) bool { return g.Key == group }) {
				r.Choice.Groups = append(r.Choice.Groups, *o.Group)
			}
		}
		r.Options = append(r.Options, ResolvedOption{Value: o.ID, Label: o.Label, Tone: "neutral", Picture: o.Picture, Group: group, NoPreference: o.ID == c.NoPreference})
	}
	if r.Control == "figure" && f.Presentation.Figure != nil {
		r.Figure = f.Presentation.Figure
	}
}

func commandFields(in Input, command CommandInput) []ResolvedField {
	in.Schema.Fields = command.Fields
	in.Immutable, in.Commands, in.CommandsOnly, in.Singleton = nil, nil, false, false
	in.Operations = []string{"create"}
	in.Hints = entity.EntryHints{}
	out := fields(in)
	for i := range out {
		out[i].Editable = ResolvedEditable{Create: true}
		out[i].OnList, out[i].OnRecord = false, false
	}
	return out
}
