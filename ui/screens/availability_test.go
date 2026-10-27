package screens

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/screen"
)

func TestScreenDeclarationsExcludeUnavailableExecutableControls(t *testing.T) {
	r := httpx.Resource{Module: "note", Entity: "note", Screen: "/app/note/notes", Write: "note:write",
		Schema: entity.Schema{Module: "note", Entity: "note"},
		Create: func(context.Context, map[string]any) (map[string]any, error) {
			t.Fatal("export ran create")
			return nil, nil
		},
		Commands: []httpx.Command{
			{Verb: "empty", Run: func(context.Context, uuid.UUID, map[string]any) error { t.Fatal("export ran a command"); return nil }},
			{Verb: "unwired"},
			{Verb: "system", Present: entity.CommandHints{System: true}, Run: func(context.Context, uuid.UUID, map[string]any) error { return nil }},
		},
	}
	views := declaredViews(r)
	refs := func(views []screen.View) []string {
		out := []string{}
		for _, view := range views {
			out = append(out, string(view.Ref()))
		}
		return out
	}
	want := []string{"app/note/resource/note/list", "app/note/resource/note/detail", "app/note/resource/note/create", "app/note/resource/note/command/empty"}
	if got := refs(views); !slices.Equal(got, want) {
		t.Fatalf("views = %v, want %v", got, want)
	}
	command := views[len(views)-1]
	if len(command.Anchors()) != 1 || command.Anchors()[0].Name() != "command.empty" {
		t.Fatalf("argumentless command = %v", command)
	}
	r.Create = nil
	views = declaredViews(r)
	if slices.Contains(refs(views), "app/note/resource/note/create") || len(views[0].Anchors()) != 0 {
		t.Fatalf("unwired create survived: %v", views)
	}
	r.CommandsOnly = true
	if views = declaredViews(r); len(views) != 0 {
		t.Fatalf("hostless command views = %v", views)
	}
}
