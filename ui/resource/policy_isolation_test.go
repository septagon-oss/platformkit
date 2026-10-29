package resource_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func TestResolvedBlocksKeepEqualLabelsAndEmptyMembersDistinct(t *testing.T) {
	r := note()
	r.Schema.Fields = []entity.Field{{Name: "title", Type: entity.TypeString}, {Name: "first", Type: entity.TypeTime}, {Name: "second", Type: entity.TypeTime}, {Name: "empty", Type: entity.TypeString}}
	p := present.Resolve(present.Input{Schema: r.Schema})
	p.Fields[1].Label, p.Fields[2].Label = "When", "When"
	p.Fields[3].Reading.Missing = ""
	p.Layout.Overview = []string{"first"}
	p.Layout.Sections = []present.ResolvedSection{{Key: "dates", Label: "Dates", Fields: []string{"second"}}, {Key: "blank", Label: "Blank", Fields: []string{}}}
	p.Layout.Information = []string{"empty"}
	r.Resolved = &p
	out := render(t, resource.Detail(r, opts, map[string]any{"title": "Dates", "first": "2026-07-01T00:30:00Z", "second": "2026-07-02T00:30:00Z"}, false).Body)
	for _, word := range []string{"2026-07-01 00:30", "2026-07-02 00:30", ">Empty<", ">Blank<"} {
		if !strings.Contains(out, word) {
			t.Errorf("missing %s", word)
		}
	}
	if strings.Index(out, "2026-07-01 00:30") > strings.Index(out, "2026-07-02 00:30") || strings.Count(out, ">When<") != 2 {
		t.Fatal("equal labels changed membership/order")
	}
}

func TestParallelRendersKeepTheirLanguageCommandsAndEnvironment(t *testing.T) {
	r := note()
	r.Schema.Fields = []entity.Field{{Name: "title", Type: entity.TypeString}, {Name: "enabled", Type: entity.TypeBool}, {Name: "at", Type: entity.TypeTime}}
	r.Commands = []resource.Command{{Verb: "run", Label: "Run", Collection: true, Base: r.Screen}}
	before, _ := json.Marshal(r)
	t.Cleanup(func() {
		after, _ := json.Marshal(r)
		if string(before) != string(after) {
			t.Error("shared resource mutated")
		}
	})
	for _, language := range []string{"en", "pt-PT"} {
		for _, allowed := range []bool{false, true} {
			for _, zone := range []string{"UTC", "America/Los_Angeles"} {
				t.Run(language+"/"+zone+"/"+map[bool]string{true: "allowed", false: "withheld"}[allowed], func(t *testing.T) {
					t.Parallel()
					local := r
					if !allowed {
						local.Commands = nil
					}
					o := opts
					o.Location, _ = time.LoadLocation(zone)
					o.Rendered = time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
					word := "Yes"
					if language == "pt-PT" {
						word = "Sim"
					}
					o.Locale = speak(language, spoken{words: map[string]string{"screens.true": word}})
					out := render(t, resource.List(local, o, []map[string]any{{"id": "one", "title": "Shared", "enabled": true, "at": "2026-07-01T00:30:00Z"}}, 1, 1, "", false).Body)
					clock := "2026-07-01 00:30"
					if zone != "UTC" {
						clock = "2026-06-30 17:30"
					}
					if !strings.Contains(out, ">"+word+"<") || !strings.Contains(out, clock) || strings.Contains(out, `aria-label="Run"`) != allowed {
						t.Fatal("render crossed request inputs")
					}
				})
			}
		}
	}
}
