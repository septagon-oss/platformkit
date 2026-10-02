package seed

import (
	"testing"
	"time"
)

func TestRelativeDateRejectsNegativeClockParts(t *testing.T) {
	now := time.Date(2026, time.October, 1, 10, 30, 0, 0, time.UTC)
	for _, expression := range []string{"monday -1:00", "monday 00:-1"} {
		if got, err := ResolveDate(now, expression, false); err == nil {
			t.Errorf("ResolveDate(%q) = %s; want an invalid-clock refusal", expression, got.Format(time.RFC3339))
		}
	}
}
