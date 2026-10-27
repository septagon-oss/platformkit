package screen_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/screen"
)

func TestReferencesPreserveCanonicalNamesAndPathBoundaries(t *testing.T) {
	view, err := screen.Generic("app", "feature/A", "Résumé", "create", "")
	if err != nil || view != "app/feature%2FA/resource/R%C3%A9sum%C3%A9/create" {
		t.Fatalf("view = %q, %v", view, err)
	}
	flat, _ := view.Anchor("field", "values.title")
	nested, _ := view.Anchor("field", "values", "title")
	if flat == nested || !strings.HasSuffix(string(flat), "#field.values%2Etitle") || !strings.HasSuffix(string(nested), "#field.values.title") {
		t.Fatalf("paths collided: %s, %s", flat, nested)
	}
	for _, ref := range []string{"", "app/a/custom/", "app/%61/custom/x", "app/a/custom/%2f", "app/a/custom/%FF", "app/a/custom/%00", "app/a/custom/a.b", "app/a/resource/note/new"} {
		if _, err := screen.ParseView(ref); err == nil {
			t.Errorf("accepted invalid view %q", ref)
		}
	}
	for _, name := range []string{"", "app/a/custom/x#title", "app/a/custom/x#row.1", "app/a/custom/x#action.a.b", "app/a/custom/x#field.a..b"} {
		if _, err := screen.ParseAnchor(name); err == nil {
			t.Errorf("accepted invalid anchor %q", name)
		}
	}
}

func exampleView() screen.View {
	return screen.MustView(screen.ViewSpec{Ref: "app/note/custom/editor", Cardinality: screen.One, Anchors: []screen.AnchorSpec{
		{Name: "field.title", Control: "text", Binding: screen.Value("note-save", []string{"title"}), Interactions: []screen.Interaction{screen.Committed}},
		{Name: "action.submit", Control: "button", Binding: screen.Submission("note-save"), Interactions: []screen.Interaction{screen.Activated, screen.Submitted}},
	}})
}

func exampleMounts() []screen.Mount {
	return []screen.Mount{
		{Owner: "note", Document: true, Operation: screen.Operation{ID: "note-editor", Surface: "app", Method: "GET", Path: "/app/note/edit", Guard: "permission note:write"}, Responses: []screen.Response{{View: "app/note/custom/editor", Kind: "input"}}},
		{Owner: "note", Operation: screen.Operation{ID: "note-save", Surface: "app", Method: "POST", Path: "/api/v1/note", Guard: "permission note:write"}},
	}
}

func TestDescriptorsCopyInputsAndRefuseRepeatedAnchors(t *testing.T) {
	path := []string{"title"}
	fixed := []screen.FixedInput{{Location: "form", FieldPath: []string{"verdict"}, Value: "approved"}}
	binding := screen.Value("note-save", path, fixed...)
	path[0], fixed[0].FieldPath[0], fixed[0].Value = "secret", "different", "declined"
	interactions := []screen.Interaction{screen.Committed}
	spec := screen.ViewSpec{Ref: "app/note/custom/editor", Cardinality: screen.One, Anchors: []screen.AnchorSpec{{Name: "field.title", Control: "text", Binding: binding, Interactions: interactions}}}
	v, err := screen.NewView(spec)
	if err != nil {
		t.Fatal(err)
	}
	interactions[0], spec.Anchors[0].Name = screen.Submitted, "field.different"
	a, err := v.Anchor("app/note/custom/editor#field.title")
	if err != nil || !a.Supports(screen.Committed) || a.Binding().FieldPath[0] != "title" || a.Binding().Fixed[0].Value != "approved" {
		t.Fatalf("constructor retained caller storage: %#v, %v", a.Binding(), err)
	}
	got := a.Binding()
	got.Fixed[0].FieldPath[0] = "changed"
	if a.Binding().Fixed[0].FieldPath[0] != "verdict" {
		t.Fatal("accessor exposed retained storage")
	}
	spec.Cardinality = screen.Many
	if _, err := screen.NewView(spec); err == nil || !strings.Contains(err.Error(), "typed instance binding") {
		t.Fatalf("repeated anchor refusal = %v", err)
	}
}

func TestDescriptorsRefuseUnsupportedCapabilitiesAndDuplicateNames(t *testing.T) {
	base := screen.AnchorSpec{Name: "action.submit", Control: "button", Binding: screen.Submission("note-save"), Interactions: []screen.Interaction{screen.Submitted}}
	for _, interaction := range []screen.Interaction{screen.Committed, "clicked", ""} {
		input := base
		input.Interactions = []screen.Interaction{interaction}
		if _, err := screen.NewAnchor(input); err == nil {
			t.Errorf("accepted %q", interaction)
		}
	}
	base.Interactions = []screen.Interaction{screen.Submitted, screen.Submitted}
	if _, err := screen.NewAnchor(base); err == nil {
		t.Fatal("accepted duplicate interaction")
	}
	base.Interactions = nil
	if _, err := screen.NewView(screen.ViewSpec{Ref: "app/note/custom/editor", Cardinality: screen.One, Anchors: []screen.AnchorSpec{base, base}}); err == nil {
		t.Fatal("accepted duplicate anchor")
	}
}

func TestInventoryRequiresActualMountsAndTargets(t *testing.T) {
	view, mounts := exampleView(), exampleMounts()
	for name, candidate := range map[string][]screen.Mount{"missing view": mounts[1:], "missing target": mounts[:1], "duplicate operation": append(append([]screen.Mount{}, mounts...), mounts[0])} {
		t.Run(name, func(t *testing.T) {
			if _, err := screen.Publish("example", 1, []screen.View{view}, candidate); err == nil {
				t.Fatal("accepted invalid composition")
			}
		})
	}
	mounts[0].Owner = "other"
	if _, err := screen.Publish("example", 1, []screen.View{view}, mounts); err == nil {
		t.Fatal("accepted cross-owner view")
	}
	mounts = exampleMounts()
	mounts[0].Responses = nil
	if _, err := screen.Publish("example", 1, nil, mounts); err == nil {
		t.Fatal("accepted unclassified document route")
	}
}

func TestSnapshotIsDeterministicAndRejectsCorruption(t *testing.T) {
	mounts := exampleMounts()
	first, err := screen.Publish("example", 1, []screen.View{exampleView()}, mounts)
	if err != nil {
		t.Fatal(err)
	}
	mounts[0], mounts[1] = mounts[1], mounts[0]
	second, err := screen.Publish("example", 1, []screen.View{exampleView()}, mounts)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := first.Bytes()
	b, _ := second.Bytes()
	if !bytes.Equal(a, b) {
		t.Fatal("registration order changed artifact")
	}
	parsed, err := screen.ParseSnapshot(a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parsed.Anchor("app/note/custom/editor#action.submit"); err != nil {
		t.Fatal(err)
	}
	if _, err := parsed.Anchor("app/note/custom/editor#action.missing"); err == nil {
		t.Fatal("missing anchor resolved")
	}
	for name, corrupted := range map[string][]byte{
		"duplicate key":       bytes.Replace(a, []byte(`"app": "example"`), []byte(`"app": "example", "app": "example"`), 1),
		"unknown key":         bytes.Replace(a, []byte(`"app": "example"`), []byte(`"app": "example", "extra": true`), 1),
		"unsupported version": bytes.Replace(a, []byte(`"schemaVersion": 1`), []byte(`"schemaVersion": 2`), 1),
		"changed field":       bytes.Replace(a, []byte(`"title"`), []byte(`"different"`), 1),
		"trailing object":     append(append([]byte{}, a...), []byte(`{}`)...),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := screen.ParseSnapshot(corrupted); err == nil {
				t.Fatal("accepted corrupt artifact")
			}
		})
	}
	detached := first.Snapshot()
	detached.Views[0].Anchors[0].Name = "action.changed"
	if again, _ := first.Bytes(); !bytes.Equal(a, again) {
		t.Fatal("snapshot accessor exposed storage")
	}
}

func TestPublishedIdentityCannotBeRetargeted(t *testing.T) {
	old, err := screen.Publish("example", 1, []screen.View{exampleView()}, exampleMounts())
	if err != nil {
		t.Fatal(err)
	}
	mounts := exampleMounts()
	mounts[1].Operation.Path = "/api/v1/other"
	changed, err := screen.Publish("example", 1, []screen.View{exampleView()}, mounts)
	if err != nil {
		t.Fatal(err)
	}
	if err := changed.Compatible(old); err == nil {
		t.Fatal("retargeted a published anchor")
	}
	mounts = exampleMounts()
	mounts[1].Operation.Guard = "permission note:manage"
	changed, err = screen.Publish("example", 1, []screen.View{exampleView()}, mounts)
	if err != nil {
		t.Fatal(err)
	}
	if err := changed.Compatible(old); err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(old.Snapshot())
	b, _ := json.Marshal(changed.Snapshot())
	if bytes.Equal(a, b) {
		t.Fatal("guard change did not change snapshot")
	}
}

func ExampleNewView() {
	view := screen.MustView(screen.ViewSpec{Ref: "app/notes/custom/new", Cardinality: screen.One, Anchors: []screen.AnchorSpec{
		{Name: "action.submit", Control: "button", Binding: screen.Submission("notes-create"), Interactions: []screen.Interaction{screen.Submitted}},
	}})
	a, err := view.Anchor("app/notes/custom/new#action.submit")
	if err != nil {
		panic(err)
	}
	fmt.Println(a.Supports(screen.Submitted), a.Supports(screen.Committed))
	// Output: true false
}
