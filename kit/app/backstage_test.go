package app

// backstage_test.go reads the stream back the way a catalog would: one document
// at a time, by kind and name, with the spec's members as a map. The structs that
// wrote it are not consulted, because a projection checked against its own types
// proves only that the encoder ran.

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// catalogDoc is a document of the stream as a reader sees it, spec and all.
type catalogDoc struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec map[string]any `yaml:"spec"`
}

// backstageFixture is a composition whose modules differ in every way a catalog
// entry turns on: one that emits and hears one of its own events, one that only
// emits, one that hears everything and emits nothing, one that serves and hears
// nothing, and the label for what no module registered.
func backstageFixture() Description {
	route := func(id string) []RouteDescription {
		return []RouteDescription{{OperationID: id, Method: "GET", Path: "/" + id}}
	}
	return Description{Modules: []ModuleDescription{
		{Name: "task", Events: []EventDescription{{Name: "task.assigned"}, {Name: "task.untyped"}},
			Subscriptions: []string{"task.assigned"}, Routes: route("task")},
		{Name: "billing", Events: []EventDescription{{Name: "billing.plan.created"}}, Routes: route("billing")},
		{Name: "audit", Subscriptions: []string{"task.assigned", "task.untyped", "billing.plan.created"}, Routes: route("audit")},
		{Name: "web", Routes: route("index")},
		{Name: kernelModule, Routes: route("openapi")},
	}}
}

func TestBackstageDescribesTheComposition(t *testing.T) {
	raw, err := Backstage(backstageFixture())
	if err != nil {
		t.Fatalf("Backstage: %v", err)
	}
	docs := catalogDocs(t, raw)

	// One System, then the components by name, then the APIs by name. The kernel
	// is in none of them: it is what no module registered, not a deployable thing.
	want := [][2]string{{"System", "platformkit"},
		{"Component", "audit"}, {"Component", "billing"}, {"Component", "task"}, {"Component", "web"},
		{"API", "audit-http"}, {"API", "billing-events"}, {"API", "billing-http"},
		{"API", "task-events"}, {"API", "task-http"}, {"API", "web-http"}}
	if len(docs) != len(want) {
		t.Fatalf("%d documents, want %d: %s", len(docs), len(want), catalogNames(docs))
	}
	for i, w := range want {
		if docs[i].Kind != w[0] || docs[i].Metadata.Name != w[1] {
			t.Errorf("document %d is %s %q, want %s %q", i, docs[i].Kind, docs[i].Metadata.Name, w[0], w[1])
		}
		if docs[i].APIVersion != "backstage.io/v1alpha1" {
			t.Errorf("%s %q carries apiVersion %q, want %q", docs[i].Kind, docs[i].Metadata.Name, docs[i].APIVersion, "backstage.io/v1alpha1")
		}
	}

	// A subscription is the composition's only dependency edge: dependsOn and
	// consumesApis are one list in two vocabularies. Three subscriptions and two
	// emitting modules is the case worth naming.
	audit := specOf(t, docs, "Component", "audit")
	if got := listOf(audit["dependsOn"]); !same(got, []string{"component:billing", "component:task"}) {
		t.Errorf("audit dependsOn = %v, want the module of every event it subscribes to, once each", got)
	}
	if got := listOf(audit["consumesApis"]); !same(got, []string{"api:billing-events", "api:task-events"}) {
		t.Errorf("audit consumesApis = %v", got)
	}
	// Listening to an event of one's own is not a dependency edge. task hears
	// task.assigned and nothing else, so it depends on nobody and consumes no
	// other module's API; TestBackstageKeepsAComponentsOwnEvents… is where the rule
	// is pinned down, with a cross-module edge beside it.
	task := specOf(t, docs, "Component", "task")
	for _, field := range []string{"dependsOn", "consumesApis"} {
		if v, listed := task[field]; listed {
			t.Errorf("task lists %v under %s and subscribes only to its own events", v, field)
		}
	}
	for key, want := range map[string]string{"type": "service", "lifecycle": "production",
		"owner": "platformkit", "system": "platformkit"} {
		if got := audit[key]; got != want {
			t.Errorf("component %s = %v, want %q", key, got, want)
		}
	}
	// Events only when it emits, HTTP only when it registers routes. audit hears
	// everything and emits nothing, so it provides its routes and no event API.
	if got := listOf(audit["providesApis"]); !same(got, []string{"api:audit-http"}) {
		t.Errorf("audit providesApis = %v, want only its HTTP surface", got)
	}
	if got := listOf(task["providesApis"]); !same(got, []string{"api:task-events", "api:task-http"}) {
		t.Errorf("task providesApis = %v, want its events before its HTTP", got)
	}
	web := specOf(t, docs, "Component", "web")
	if _, hears := web["consumesApis"]; hears {
		t.Errorf("web consumes %v and subscribes to nothing", web["consumesApis"])
	}
	if got := listOf(web["providesApis"]); !same(got, []string{"api:web-http"}) {
		t.Errorf("web providesApis = %v", got)
	}

	// Both APIs are references. The documents they point at are the one the same
	// command writes and the one the application serves, not copies for the
	// catalog to keep in step.
	for _, want := range []struct{ name, kind, definition string }{
		{"task-events", "asyncapi", "$text: ./asyncapi.json"},
		{"task-http", "openapi", "$text: /openapi.json"},
	} {
		api := specOf(t, docs, "API", want.name)
		if api["type"] != want.kind || api["definition"] != want.definition {
			t.Errorf("API %q = %v, want type %q and definition %q", want.name, api, want.kind, want.definition)
		}
	}

	// A spec reads in Backstage's own order — type, lifecycle, owner, system —
	// which yaml writes from struct field order and a decoded map cannot show.
	keys := []string{"type:", "lifecycle:", "owner:", "system:", "dependsOn:", "providesApis:"}
	component := docOf(t, raw, "Component", "audit")
	at := -1
	for _, key := range keys {
		found := strings.Index(component, key)
		if found <= at {
			t.Errorf("%s is out of order in the Component spec:\n%s", key, component)
		}
		at = found
	}
}

// TestBackstageWritesOneStreamOfDocuments is the shape a catalog loader needs:
// every document its own, separated by --- and nothing else in the way.
func TestBackstageWritesOneStreamOfDocuments(t *testing.T) {
	raw, err := Backstage(backstageFixture())
	if err != nil {
		t.Fatalf("Backstage: %v", err)
	}
	if got, want := strings.Count(string(raw), "\n---\n")+1, len(catalogDocs(t, raw)); got != want {
		t.Errorf("%d separators for %d documents", got, want)
	}
}

func catalogDocs(t *testing.T, stream []byte) []catalogDoc {
	t.Helper()
	var docs []catalogDoc
	dec := yaml.NewDecoder(bytes.NewReader(stream))
	for {
		var doc catalogDoc
		if err := dec.Decode(&doc); err == io.EOF {
			return docs
		} else if err != nil {
			t.Fatalf("the stream is not YAML: %v", err)
		}
		docs = append(docs, doc)
	}
}

func docOf(t *testing.T, stream []byte, kind, name string) string {
	t.Helper()
	for _, doc := range strings.Split(string(stream), "\n---\n") {
		if strings.Contains(doc, "kind: "+kind+"\n") && strings.Contains(doc, "  name: "+name+"\n") {
			return doc
		}
	}
	t.Fatalf("no %s named %q in the stream:\n%s", kind, name, stream)
	return ""
}

func specOf(t *testing.T, docs []catalogDoc, kind, name string) map[string]any {
	t.Helper()
	for _, d := range docs {
		if d.Kind == kind && d.Metadata.Name == name {
			return d.Spec
		}
	}
	t.Fatalf("no %s named %q in %s", kind, name, catalogNames(docs))
	return nil
}

func catalogNames(docs []catalogDoc) string {
	var out []string
	for _, d := range docs {
		out = append(out, d.Kind+" "+d.Metadata.Name)
	}
	return strings.Join(out, ", ")
}

func listOf(v any) []string {
	items, _ := v.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.(string))
	}
	return out
}

func same(got, want []string) bool {
	return len(got) == len(want) && strings.Join(got, ",") == strings.Join(want, ",")
}

// TestBackstageKeepsAComponentsOwnEventsOutOfItsDependencies: dependsOn is a
// relation to another entity, and a component that lists itself renders as a
// cycle of one. Handling an event of your own is real — modules/file removes a
// blob that way, after the transaction that removed the row commits, and modules/auth
// listens for its own reset request — but it is a fact about the surface a module
// provides, not an edge in the graph. The self edge must go and the cross-module
// edges beside it must not move.
func TestBackstageKeepsAComponentsOwnEventsOutOfItsDependencies(t *testing.T) {
	route := func(id string) []RouteDescription {
		return []RouteDescription{{OperationID: id, Method: "GET", Path: "/" + id}}
	}
	raw, err := Backstage(Description{Modules: []ModuleDescription{
		{Name: "file", Events: []EventDescription{{Name: "file.deleted"}},
			Subscriptions: []string{"file.deleted", "user.user.updated"}, Routes: route("file")},
		// note hears nothing but itself, so it gains no dependency at all.
		{Name: "note", Events: []EventDescription{{Name: "note.created"}},
			Subscriptions: []string{"note.created"}},
		{Name: "user", Events: []EventDescription{{Name: "user.user.updated"}}, Routes: route("user")},
	}})
	if err != nil {
		t.Fatalf("Backstage: %v", err)
	}
	docs := catalogDocs(t, raw)

	file := specOf(t, docs, "Component", "file")
	if got := listOf(file["dependsOn"]); !same(got, []string{"component:user"}) {
		t.Errorf("file dependsOn = %v, want component:user alone: its own event is not one of its dependencies", got)
	}
	if got := listOf(file["consumesApis"]); !same(got, []string{"api:user-events"}) {
		t.Errorf("file consumesApis = %v, want api:user-events alone", got)
	}
	if got := listOf(file["providesApis"]); !same(got, []string{"api:file-events", "api:file-http"}) {
		t.Errorf("file providesApis = %v, want its own events API and its HTTP surface", got)
	}

	note := specOf(t, docs, "Component", "note")
	for _, field := range []string{"dependsOn", "consumesApis"} {
		if v, listed := note[field]; listed {
			t.Errorf("note lists %v under %s, and it subscribes to no other module", v, field)
		}
	}
	if got := listOf(note["providesApis"]); !same(got, []string{"api:note-events"}) {
		t.Errorf("note providesApis = %v, want the events it emits and handles itself", got)
	}
}
