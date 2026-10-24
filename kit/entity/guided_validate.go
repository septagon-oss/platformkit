package entity

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Validate checks the complete read contract without changing its values.
// Recording must match the declaration, even when this caller cannot write.
func (s GuidedSession) Validate(recording bool) error {
	if s.ID == uuid.Nil || len(s.Steps) == 0 || len(s.Steps) > 1000 {
		return fmt.Errorf("session: identity and 1–1000 steps are required")
	}
	if err := guidedText(s.Title, 200, true); err != nil {
		return err
	}
	if err := guidedPicture(s.Picture); err != nil {
		return err
	}
	seen, total := map[uuid.UUID]bool{}, 0
	for _, step := range s.Steps {
		if seen[step.ID] {
			return fmt.Errorf("session: duplicate step")
		}
		seen[step.ID] = true
		if err := step.validate(); err != nil {
			return err
		}
		total += len(step.Sets)
	}
	if total > 10000 || recording != (s.Progress != nil) {
		return fmt.Errorf("session: invalid total or recording progress")
	}
	return s.validateProgress(total)
}

func (s GuidedStep) validate() error {
	if s.ID == uuid.Nil || len(s.Sets) == 0 || len(s.Sets) > 1000 || len(s.Items) > 100 {
		return fmt.Errorf("session: invalid step identity, sets or items")
	}
	for _, text := range []struct {
		value    string
		limit    int
		required bool
	}{{s.Title, 200, true}, {s.Text, 20000, false}} {
		if err := guidedText(text.value, text.limit, text.required); err != nil {
			return err
		}
	}
	if err := guidedPicture(s.Picture); err != nil {
		return err
	}
	seen := map[uuid.UUID]bool{}
	for _, id := range s.Sets {
		if id == uuid.Nil || seen[id] {
			return fmt.Errorf("session: sets need unique nonzero identities")
		}
		seen[id] = true
	}
	for _, item := range s.Items {
		if err := guidedText(item.Label, 200, true); err != nil {
			return err
		}
		if err := guidedText(item.Quantity, 500, false); err != nil {
			return err
		}
		if err := guidedPicture(item.Picture); err != nil {
			return err
		}
	}
	return s.validateTiming()
}

func (s GuidedStep) validateTiming() error {
	if s.RestBetweenSeconds < 0 || s.RestBetweenSeconds > 86400 || s.RestAfterSeconds < 0 || s.RestAfterSeconds > 86400 {
		return fmt.Errorf("session: rest must be 0–86400 seconds")
	}
	if len(s.Sets) == 1 && s.RestBetweenSeconds != 0 {
		return fmt.Errorf("session: one set has no between-set rest")
	}
	if t := s.Target; t != nil {
		if t.Value < 1 || (t.Kind != "repetitions" && t.Kind != "seconds") || (t.Kind == "seconds" && t.Value > 86400) {
			return fmt.Errorf("session: invalid target")
		}
	}
	if t := s.Timer; t != nil {
		if t.Seconds < 1 || t.Seconds > 86400 {
			return fmt.Errorf("session: timer must be 1–86400 seconds")
		}
		return guidedText(t.Name, 100, true)
	}
	return nil
}

func (s GuidedSession) validateProgress(total int) error {
	p := s.Progress
	if p == nil {
		return nil
	}
	if len(p.Completed) > total {
		return fmt.Errorf("session: excess completions")
	}
	for i, receipt := range p.Completed {
		step, set, _ := s.Pair(i)
		if receipt.SessionID != s.ID || receipt.StepID != s.Steps[step].ID || receipt.SetID != s.Steps[step].Sets[set] || receipt.Revision != int64(i+2) || !guidedUTC(receipt.CompletedAt) {
			return fmt.Errorf("session: completions must be an ordered, revisioned UTC prefix")
		}
	}
	revision := int64(len(p.Completed) + 1)
	if p.Finished != nil {
		revision++
		if err := p.Finished.Validate(s.ID, revision, len(p.Completed), total); err != nil {
			return err
		}
	}
	if p.Revision != revision {
		return fmt.Errorf("session: incoherent revision")
	}
	return nil
}

// Validate checks a summary against the authoritative session and prefix counts.
func (s GuidedSummary) Validate(id uuid.UUID, revision int64, completed, total int) error {
	if s.SessionID != id || s.Revision != revision || s.CompletedSets != int32(completed) || s.TotalSets != int32(total) || !guidedUTC(s.FinishedAt) || len(s.Facts) > 100 {
		return fmt.Errorf("session: incoherent finish summary")
	}
	if err := guidedText(s.Title, 200, true); err != nil {
		return err
	}
	if err := guidedText(s.Text, 20000, false); err != nil {
		return err
	}
	for _, fact := range s.Facts {
		if err := guidedText(fact.Label, 200, true); err != nil {
			return err
		}
		if err := guidedText(fact.Value, 500, false); err != nil {
			return err
		}
	}
	return nil
}

// Pair locates a zero-based flattened set position. An out-of-range position
// returns ok=false; callers use that boundary to offer an explicit finish.
func (s GuidedSession) Pair(position int) (step, set int, ok bool) {
	if position < 0 {
		return 0, 0, false
	}
	for i, step := range s.Steps {
		if position < len(step.Sets) {
			return i, position, true
		}
		position -= len(step.Sets)
	}
	return 0, 0, false
}

func guidedText(value string, limit int, required bool) error {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > limit || (required && strings.TrimSpace(value) == "") {
		return fmt.Errorf("session: invalid text (maximum %d characters)", limit)
	}
	return nil
}

func guidedPicture(p Picture) error {
	if p == "" {
		return nil
	}
	_, err := PictureKey(p)
	return err
}

func guidedUTC(t time.Time) bool {
	_, offset := t.Zone()
	return !t.IsZero() && offset == 0
}
