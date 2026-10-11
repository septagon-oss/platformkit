package seed

import (
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestLoadRejectsAmbiguousOrUnsafeInputBeforeWriting(t *testing.T) {
	for _, tc := range []struct {
		name, body, other, want string
	}{
		{"duplicate YAML mapping", "apiVersion: platformkit.seed/v1\nresource: contents\nrecords:\n  - key: home\n    fields: {title: One, title: Two}\n", "", "duplicate key"},
		{"unknown field", "apiVersion: platformkit.seed/v1\nresource: contents\nrecords:\n  - key: home\n    mystery: value\n", "", "unknown field"},
		{"password field", "apiVersion: platformkit.seed/v1\nresource: contents\nrecords:\n  - key: home\n    fields: {password: secret}\n", "", "passwords must come from the application"},
		{"duplicate record", "apiVersion: platformkit.seed/v1\nresource: contents\nrecords:\n  - key: home\n  - key: home\n", "", "duplicates contents/home"},
		{"multiple documents", "apiVersion: platformkit.seed/v1\nresource: contents\nrecords: []\n---\nresource: contents\n", "", "multiple documents"},
		{"unsafe asset", "apiVersion: platformkit.seed/v1\nresource: contents\nrecords:\n  - key: home\n    asset: ../secret\n", "", "unsafe asset"},
		{"same resource in two formats", "apiVersion: platformkit.seed/v1\nresource: contents\nrecords: []\n", `{"apiVersion":"platformkit.seed/v1","resource":"contents","records":[]}`, "both define contents"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := fstest.MapFS{"seed/starter/contents.yaml": {Data: []byte(tc.body)}}
			if tc.other != "" {
				files["seed/starter/contents.json"] = &fstest.MapFile{Data: []byte(tc.other)}
			}
			_, err := Load(files, "seed", "starter")
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "seed/starter/contents") {
				t.Fatalf("Load error = %v; want %q at source", err, tc.want)
			}
		})
	}
}

func TestLoadPreservesSourcesAndKeepsKindsSeparate(t *testing.T) {
	files := fstest.MapFS{
		"seed/starter/contents.yaml": {Data: []byte("apiVersion: platformkit.seed/v1\nresource: contents\nprune: True\nrecords:\n  - key: home\n    fields: {title: Home, published: true}\n")},
		"seed/demo/tasks.json":       {Data: []byte(`{"apiVersion":"platformkit.seed/v1","resource":"tasks","records":[{"key":"tour","fields":{"title":"Take the tour"}}]}`)},
	}
	docs, err := Load(files, "seed", "starter", "demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 || !docs[0].Prune || docs[0].Records[0].Source.Line != 5 || docs[0].Records[0].Fields["published"] != true || docs[1].Records[0].Key != "tour" {
		t.Fatalf("unexpected parsed documents: %+v", docs)
	}
	if docs[0].Records[0].Values["fields/title"].Line != 6 {
		t.Fatalf("title source = %+v", docs[0].Records[0].Values["fields/title"])
	}
}

func TestOrderUsesDeclaredReferencesAndNamesRefusals(t *testing.T) {
	files := fstest.MapFS{
		"seed/demo/tasks.yaml": {Data: []byte("apiVersion: platformkit.seed/v1\nresource: tasks\nrecords:\n  - key: tour\n    commands:\n      - {name: assign, user: 'users/marta@example.test'}\n")},
		"seed/demo/users.yaml": {Data: []byte("apiVersion: platformkit.seed/v1\nresource: users\nrecords:\n  - key: marta@example.test\n    fields: {displayName: Marta}\n")},
	}
	docs, err := Load(files, "seed", "demo")
	if err != nil {
		t.Fatal(err)
	}
	refs := []Reference{{Resource: "tasks", Path: "commands/assign/user", Target: "users"}}
	ordered, err := Order(docs, refs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ordered) != 2 || ordered[0].Resource != "users" || ordered[1].Resource != "tasks" {
		t.Fatalf("order = %+v", ordered)
	}
	missing := []Document{{Resource: "tasks", Records: []Record{{Key: "tour", Fields: map[string]any{"owner": "users/absent"}, Source: Source{File: "tasks.yaml", Line: 4}}}}}
	_, err = Order(missing, []Reference{{Resource: "tasks", Path: "fields/owner", Target: "users"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "tasks.yaml:4") || !strings.Contains(err.Error(), "users/absent") {
		t.Fatalf("missing reference = %v", err)
	}
	cycle := []Document{{Resource: "nodes", Records: []Record{
		{Key: "a", Fields: map[string]any{"next": "nodes/b"}, Source: Source{File: "nodes.yaml", Line: 4}},
		{Key: "b", Fields: map[string]any{"next": "nodes/a"}, Source: Source{File: "nodes.yaml", Line: 6}},
	}}}
	_, err = Order(cycle, []Reference{{Resource: "nodes", Path: "fields/next", Target: "nodes"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "cycle") || !strings.Contains(err.Error(), "nodes/a") || !strings.Contains(err.Error(), "nodes/b") {
		t.Fatalf("cycle = %v", err)
	}
}

func TestRelativeDatesUseOneUTCClock(t *testing.T) {
	now := time.Date(2026, 10, 1, 10, 30, 0, 0, time.UTC)
	for _, tc := range []struct {
		expression string
		dateOnly   bool
		want       string
	}{
		{"+3d", false, "2026-10-04T10:30:00Z"},
		{"-2y", true, "2024-10-01T00:00:00Z"},
		{"monday 09:00", false, "2026-10-05T09:00:00Z"},
	} {
		got, err := ResolveDate(now, tc.expression, tc.dateOnly)
		if err != nil || got.Format(time.RFC3339) != tc.want {
			t.Errorf("%q: got %s, %v; want %s", tc.expression, got.Format(time.RFC3339), err, tc.want)
		}
	}
	got, err := ResolveDate(time.Date(2024, 2, 29, 12, 0, 0, 0, time.UTC), "+1y", false)
	if err != nil || got.Format(time.RFC3339) != "2025-02-28T12:00:00Z" {
		t.Errorf("leap year clamp: %s, %v", got, err)
	}
	// One sign belongs to the expression; a second belongs to nothing. Atoi
	// would have read it as part of the amount, so a doubled sign moved the day
	// in the direction the second character chose.
	for _, expression := range []string{"monday 24:00", "++3d", "-+3d"} {
		if _, err := ResolveDate(now, expression, false); err == nil {
			t.Fatalf("%q accepted: a doubled sign is not an amount", expression)
		}
	}
}

func TestDecideChangesOnlyManagedValues(t *testing.T) {
	current := Snapshot{Present: true,
		Fields:   map[string]any{"title": "Before", "humanNote": "keep"},
		Commands: map[string]any{"publish": true},
	}
	target := Target{Fields: map[string]any{"title": "After"}, Commands: map[string]any{"publish": true}}
	got := Decide(current, target)
	if got.Action != Update || !reflect.DeepEqual(got.Changed, []string{"fields/title"}) {
		t.Fatalf("decision = %+v", got)
	}
	current.Fields["title"] = "After"
	if got := Decide(current, target); got.Action != Unchanged || len(got.Changed) != 0 {
		t.Fatalf("unchanged decision = %+v", got)
	}
	if got := Decide(Snapshot{}, target); got.Action != Create {
		t.Fatalf("new row decision = %+v", got)
	}
}
