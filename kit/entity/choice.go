package entity

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// ChoiceSet declares finite ordered answers, independently of their appearance.
// Zero values are invalid. Validate before using a declaration for its lifetime.
type ChoiceSet struct {
	Cardinality     string         `json:"cardinality"`
	Options         []ChoiceOption `json:"options"`
	Empty           ChoiceEmpty    `json:"empty"`
	SearchThreshold int            `json:"searchThreshold"`
	Maximum         *ChoiceMaximum `json:"maximum,omitempty"`
	NoPreference    string         `json:"noPreference,omitempty"`
}

// ChoiceOption is a stable field-local ID with display words and optional media.
type ChoiceOption struct {
	ID      string         `json:"id"`
	Label   string         `json:"label"`
	Picture *ChoicePicture `json:"picture,omitempty"`
	Group   *ChoiceGroup   `json:"group,omitempty"`
}

type ChoiceGroup struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// ChoicePicture admits a stored file UUID, composed reference or #RRGGBB colour.
type ChoicePicture struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type ChoiceEmpty struct {
	Mode    string `json:"mode"`
	Message string `json:"message"`
}

type ChoiceMaximum struct {
	Count     int    `json:"count"`
	Behaviour string `json:"behaviour"`
	Message   string `json:"message"`
}

// ChoiceError names the invalid declaration member or answer and its correction.
type ChoiceError struct{ Member, Message string }

func (e *ChoiceError) Error() string           { return e.Message }
func choiceError(member, message string) error { return &ChoiceError{member, message} }

var choiceID = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
var choiceColour = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

func choiceWords(s string, max int) bool {
	return strings.TrimSpace(s) != "" && utf8.ValidString(s) && utf8.RuneCountInString(s) <= max && !strings.ContainsFunc(s, unicode.IsControl)
}

// Validate refuses inconsistent declarations without changing caller-owned data.
func (c ChoiceSet) Validate() error {
	if c.Cardinality != "one" && c.Cardinality != "many" {
		return choiceError("cardinality", "Choose cardinality one or many.")
	}
	if len(c.Options) == 0 || len(c.Options) > 256 {
		return choiceError("options", "Declare 1–256 choices.")
	}
	if c.SearchThreshold < 0 || c.SearchThreshold > 256 {
		return choiceError("searchThreshold", "Search threshold must be 0–256.")
	}
	if c.Empty.Mode != "allow" && c.Empty.Mode != "refuse" {
		return choiceError("empty", "Declare whether an empty answer is allowed or refused.")
	}
	if (c.Empty.Mode == "allow" && c.Empty.Message != "") || (c.Empty.Mode == "refuse" && !choiceWords(c.Empty.Message, 512)) {
		return choiceError("empty.message", "Supply a refusal sentence only when empty is refused.")
	}
	ids, groups := map[string]bool{}, map[string]string{}
	for _, o := range c.Options {
		if !choiceID.MatchString(o.ID) || ids[o.ID] {
			return choiceError("options.id", "Use unique choice IDs matching [a-z][a-z0-9_-]{0,31}.")
		}
		ids[o.ID] = true
		if !choiceWords(o.Label, 160) {
			return choiceError("options.label", "Supply a choice label of at most 160 characters without controls.")
		}
		if o.Group != nil {
			g := o.Group
			if !choiceID.MatchString(g.Key) || !choiceWords(g.Label, 160) || (groups[g.Key] != "" && groups[g.Key] != g.Label) {
				return choiceError("options.group", "Use a valid group key with one consistent label.")
			}
			groups[g.Key] = g.Label
		}
		if o.Picture != nil {
			if err := o.Picture.Validate(); err != nil {
				return err
			}
		}
		if o.ID == c.NoPreference && (o.Picture != nil || o.Group != nil) {
			return choiceError("noPreference", "No preference has neither picture nor group.")
		}
	}
	ordinary := len(ids)
	if c.NoPreference != "" {
		if !ids[c.NoPreference] {
			return choiceError("noPreference", "No preference must name a declared choice.")
		}
		ordinary--
	}
	if ordinary == 0 {
		return choiceError("options", "Declare at least one ordinary choice.")
	}
	return c.validateMaximum(ordinary)
}

func (c ChoiceSet) validateMaximum(ordinary int) error {
	m := c.Maximum
	if m == nil {
		return nil
	}
	if c.Cardinality != "many" || m.Count < 1 || m.Count > ordinary {
		return choiceError("maximum.count", "A many-choice maximum must be within the ordinary choice count.")
	}
	if m.Behaviour != "refuse" && m.Behaviour != "replace-oldest" {
		return choiceError("maximum.behaviour", "Choose refuse or replace-oldest at the maximum.")
	}
	if (m.Behaviour == "refuse" && !choiceWords(m.Message, 512)) || (m.Behaviour == "replace-oldest" && m.Message != "") {
		return choiceError("maximum.message", "Supply a refusal sentence only for a refusing maximum.")
	}
	return nil
}

// Validate checks reference syntax only; image delivery still authorizes access.
func (p ChoicePicture) Validate() error {
	switch p.Kind {
	case "colour":
		if choiceColour.MatchString(p.Value) {
			return nil
		}
	case "file":
		id, err := uuid.Parse(p.Value)
		if err == nil && id != uuid.Nil && id.String() == p.Value {
			return nil
		}
	case "composed":
		if _, err := PictureKey(Picture(p.Value)); err == nil {
			return nil
		}
	}
	return choiceError("options.picture", "Use a canonical file, composed picture or #RRGGBB colour.")
}

// Clone returns detached metadata with canonical colour spelling.
func (c ChoiceSet) Clone() *ChoiceSet {
	c.Options = slices.Clone(c.Options)
	if c.Maximum != nil {
		c.Maximum = new(*c.Maximum)
	}
	for i := range c.Options {
		o := &c.Options[i]
		if o.Group != nil {
			o.Group = new(*o.Group)
		}
		if o.Picture != nil {
			o.Picture = new(*o.Picture)
			if o.Picture.Kind == "colour" {
				o.Picture.Value = strings.ToUpper(o.Picture.Value)
			}
		}
	}
	return &c
}

// DisplayOptions returns detached options: ungrouped first, groups in first-seen
// order, then No preference. The same order governs native batch additions.
func (c ChoiceSet) DisplayOptions() []ChoiceOption {
	c = *c.Clone()
	keys := []string{""}
	for _, o := range c.Options {
		if o.Group != nil && !slices.Contains(keys, o.Group.Key) {
			keys = append(keys, o.Group.Key)
		}
	}
	out := make([]ChoiceOption, 0, len(c.Options))
	for _, key := range keys {
		for _, o := range c.Options {
			group := ""
			if o.Group != nil {
				group = o.Group.Key
			}
			if group == key && o.ID != c.NoPreference {
				out = append(out, o)
			}
		}
	}
	for _, o := range c.Options {
		if o.ID == c.NoPreference {
			out = append(out, o)
		}
	}
	return out
}

// ChoiceIDs reads a canonical scalar/list wire shape without coercing null or
// splitting text. It validates shape only; ChoiceSet decides admissible answers.
func (c ChoiceSet) ChoiceIDs(value any) ([]string, error) {
	if c.Cardinality == "one" {
		if s, ok := value.(string); ok {
			if s == "" {
				return []string{}, nil
			}
			return []string{s}, nil
		}
	} else {
		switch v := value.(type) {
		case []string:
			if v != nil {
				return slices.Clone(v), nil
			}
		case []any:
			if v == nil {
				break
			}
			out := make([]string, len(v))
			for i, item := range v {
				s, ok := item.(string)
				if !ok {
					return nil, errors.New("Each choice must be a string ID.")
				}
				out[i] = s
			}
			return out, nil
		}
	}
	return nil, fmt.Errorf("Supply %s choice as %s.", c.Cardinality, map[string]string{"one": "a string", "many": "an array of strings"}[c.Cardinality])
}
