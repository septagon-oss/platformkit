package entity

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// DeckHints declares a review queue using the list's filter and sort grammar.
// Nil Queue reads all rows; Order must name a declared sortable scalar field.
// Directions must contain an eligible row command or a down-only local skip.
type DeckHints struct {
	// Undoable requires every direction to declare an authoritative undo command.
	Undoable   bool
	Queue      []string
	Order      string
	Directions map[string]DeckDirection
}

// DeckDirection maps a gesture to a command with declared copy and vocabulary.
// Skip is valid only for down and excludes every other field. It sends no command.
type DeckDirection struct {
	Command string
	Label   string
	Tone    string
	Icon    string
	Skip    bool
}

// Clone returns independent queue and direction storage for request-local edits.
// A nil receiver returns nil.
func (d *DeckHints) Clone() *DeckHints {
	if d == nil {
		return nil
	}
	out := *d
	out.Queue = slices.Clone(d.Queue)
	out.Directions = maps.Clone(d.Directions)
	return &out
}

// DeckFault returns the first invalid deck path and value, or an empty string.
// It checks declaration shape and list fields; command references need the
// complete resource registration and are checked by kit/httpx.
func DeckFault(p EntryHints, fields []Field) string {
	d := p.Deck
	if d == nil {
		return ""
	}
	if fault := deckQueueFault(d.Queue, fields); fault != "" {
		return fault
	}
	if fault := deckOrderFault(d.Order, p.Sortable, fields); fault != "" {
		return fault
	}
	return deckDirectionsFault(d.Directions)
}

func deckQueueFault(queue []string, fields []Field) string {
	if queue != nil && len(queue) == 0 {
		return "deck.queue declares no filter"
	}
	for _, raw := range queue {
		name, value, ok := strings.Cut(raw, ":")
		if !ok {
			return fmt.Sprintf("deck.queue filter %q is not field:value", raw)
		}
		f, known := FieldNamed(fields, name)
		if !known || !scalarField(f) {
			return fmt.Sprintf("deck.queue filter %q names no scalar field %q", raw, name)
		}
		if _, err := Coerce(f, value); err != nil {
			return "deck.queue: " + err.Error()
		}
	}
	return ""
}

func deckOrderFault(order string, sortable []string, fields []Field) string {
	name := strings.TrimPrefix(order, "-")
	f, known := FieldNamed(fields, name)
	if name == "" || strings.HasPrefix(name, "-") || strings.HasPrefix(name, "+") || !known || !scalarField(f) || f.Presentation.Hidden() || !slices.Contains(sortable, name) {
		return fmt.Sprintf("deck.order %q must name a visible, declared sortable scalar field", order)
	}
	return ""
}

func deckDirectionsFault(directions map[string]DeckDirection) string {
	if len(directions) == 0 {
		return "deck.directions declares no direction"
	}
	for _, key := range slices.Sorted(maps.Keys(directions)) {
		if !slices.Contains([]string{"right", "left", "up", "down"}, key) {
			return fmt.Sprintf("deck.directions.%s is not a direction", key)
		}
	}
	for _, key := range []string{"right", "left", "up", "down"} {
		v, ok := directions[key]
		if !ok {
			continue
		}
		path := "deck.directions." + key
		if v.Skip {
			if key != "down" || v.Command != "" || v.Label != "" || v.Tone != "" || v.Icon != "" {
				return path + ".skip requires down and no command, label, tone or icon"
			}
			continue
		}
		if v.Command == "" {
			return path + ".command is empty"
		}
		if strings.TrimSpace(v.Label) == "" {
			return path + ".label is blank"
		}
		if !ValidTone(v.Tone) {
			return fmt.Sprintf("%s.tone %q is not a tone", path, v.Tone)
		}
		if v.Icon == "" || !ValidIcon(v.Icon) {
			return fmt.Sprintf("%s.icon %q is not an icon", path, v.Icon)
		}
	}
	return ""
}

func scalarField(f Field) bool {
	return slices.Contains([]FieldType{TypeString, TypeText, TypeInt, TypeFloat, TypeBool, TypeUUID, TypeTime}, f.Type)
}
