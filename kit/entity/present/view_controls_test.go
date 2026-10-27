package present_test

import (
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
)

func TestFormViewsKeepRequiredInputsAndDistinctMapPaths(t *testing.T) {
	in := present.Input{Schema: entity.Schema{Fields: []entity.Field{
		{Name: "secret", Type: entity.TypeString, Presentation: entity.FieldHints{Visibility: "hidden"}},
		{Name: "code", Type: entity.TypeString, Required: true, Presentation: entity.FieldHints{Visibility: "hidden"}},
		{Name: "defaulted", Type: entity.TypeString, Required: true, Default: "known", Presentation: entity.FieldHints{Visibility: "hidden"}},
		{Name: "values.title", Type: entity.TypeString},
		{Name: "values", Type: entity.TypeMap, Elem: entity.TypeString, Keys: []entity.MapKey{{Name: "title"}, {Name: "revision", From: entity.KeyFromRevision}}},
	}}}
	for _, view := range present.Resolve(in).Views {
		if view.Kind != "create" && view.Kind != "edit" {
			continue
		}
		paths := [][]string{}
		for _, control := range view.Controls {
			if control.Name == "field" {
				paths = append(paths, control.Path)
			}
		}
		want := [][]string{{"code"}, {"values.title"}, {"values", "title"}}
		if !slices.EqualFunc(paths, want, slices.Equal[[]string]) {
			t.Fatalf("%s paths = %v", view.Kind, paths)
		}
	}
}
