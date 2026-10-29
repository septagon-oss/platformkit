package entity

import "slices"

// ChoiceAction is one pick, remove or clear. Only clear has an empty ID.
type ChoiceAction struct{ Kind, ID string }

// Answer validates a new answer and returns detached IDs in retained order.
// Replacement keeps the newest suffix only after every input ID is validated.
func (c ChoiceSet) Answer(ids []string) ([]string, error) { return c.normalize(ids, true) }

// Change validates a submitted update. An unchanged empty stored answer is valid;
// removing the last answer obeys Empty. Refusals return no candidate.
func (c ChoiceSet) Change(current, proposed []string) ([]string, error) {
	return c.normalize(proposed, len(current) > 0)
}

func (c ChoiceSet) normalize(ids []string, require bool) ([]string, error) {
	if err := c.checkIDs(ids); err != nil {
		return nil, err
	}
	if c.Cardinality == "one" && len(ids) > 1 {
		return nil, choiceError("answer", "Choose at most one option.")
	}
	if len(ids) > 1 && slices.Contains(ids, c.NoPreference) {
		return nil, choiceError("answer", "No preference must be the only choice.")
	}
	if require && len(ids) == 0 && c.Empty.Mode == "refuse" {
		return nil, choiceError("answer", c.Empty.Message)
	}
	if m := c.Maximum; m != nil && len(ids) > m.Count {
		if m.Behaviour == "refuse" {
			return nil, choiceError("answer", m.Message)
		}
		ids = ids[len(ids)-m.Count:]
	}
	return append([]string{}, ids...), nil
}

func (c ChoiceSet) checkIDs(ids []string) error {
	if len(ids) > 256 {
		return choiceError("answer", "Supply at most 256 choice IDs.")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] || !slices.ContainsFunc(c.Options, func(o ChoiceOption) bool { return o.ID == id }) {
			return choiceError("answer", "Use each declared choice ID at most once.")
		}
		seen[id] = true
	}
	return nil
}

// Apply advances a canonical selection or empty draft without mutating it.
// Picking a retained ID and removing an absent ID are idempotent no-ops.
func (c ChoiceSet) Apply(current []string, action ChoiceAction) ([]string, error) {
	valid, err := c.normalize(current, false)
	if err != nil {
		return nil, err
	}
	if !slices.Equal(valid, current) {
		return nil, choiceError("current", "Repair the noncanonical stored selection before changing it.")
	}
	next := append([]string{}, current...)
	if action.Kind == "clear" {
		if action.ID != "" {
			return nil, choiceError("action", "Clear takes no choice ID.")
		}
		return c.Change(current, []string{})
	}
	if err := c.checkIDs([]string{action.ID}); err != nil {
		return nil, err
	}
	switch action.Kind {
	case "pick":
		if slices.Contains(next, action.ID) {
			return next, nil
		}
		if c.Cardinality == "one" || action.ID == c.NoPreference || slices.Contains(next, c.NoPreference) {
			next = []string{}
		}
		next = append(next, action.ID)
	case "remove":
		next = slices.DeleteFunc(next, func(id string) bool { return id == action.ID })
	default:
		return nil, choiceError("action", "Use pick, remove or clear.")
	}
	return c.Change(current, next)
}
