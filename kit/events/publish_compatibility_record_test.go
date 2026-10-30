package events_test

import (
	"os"
	"strings"
	"testing"
)

func TestPublishDeprecationHasACompatibilityRecord(t *testing.T) {
	contents, err := os.ReadFile("../../docs/compatibility/v3.0.0.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(contents), "\n") {
		if strings.HasPrefix(line, "| `kit/events.Publish` |") && strings.Contains(line, "events.PublishTo") {
			return
		}
	}
	t.Fatal("kit/events.Publish is deprecated without a compatibility record naming events.PublishTo")
}
