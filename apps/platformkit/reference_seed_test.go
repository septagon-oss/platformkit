package main

import (
	"os"
	"testing"

	"github.com/septagon-oss/platformkit/kit/seed"
)

// TestTheReferenceAppShipsAStarterAndADemoSeed pins the reference app's own
// seed, at the place docs/seed.md puts it (apps/platformkit/seed/<kind>): a
// starter holding a home and an about page, and a demo holding three people
// and five pages, so a new tenant opens on something rather than nothing.
func TestTheReferenceAppShipsAStarterAndADemoSeed(t *testing.T) {
	keys := func(kind string) (map[string]int, map[string]bool) {
		docs, err := seed.Load(os.DirFS("."), "seed", kind)
		if err != nil {
			t.Fatalf("the reference app's %s seed does not load: %v", kind, err)
		}
		counts, names := map[string]int{}, map[string]bool{}
		for _, doc := range docs {
			counts[doc.Resource] += len(doc.Records)
			for _, record := range doc.Records {
				names[record.Key] = true
			}
		}
		return counts, names
	}
	_, starter := keys("starter")
	for _, page := range []string{"home", "about"} {
		if !starter[page] {
			t.Errorf("the starter seed holds no %q record", page)
		}
	}
	demo, _ := keys("demo")
	if total := len(demo); total == 0 {
		t.Error("the demo seed declares no resource")
	}
	pages, people := 0, 0
	for resource, n := range demo {
		switch resource {
		case "users", "people", "members":
			people += n
		case "contents", "pages", "content":
			pages += n
		}
	}
	if people < 3 || pages < 5 {
		t.Errorf("the demo seed holds %d people and %d pages; the brief asks for three and five", people, pages)
	}
}
