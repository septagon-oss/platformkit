package resource_test

import (
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
	"strings"
	"testing"
)

func TestChoiceCommandUsesTheConfiguredPictureDoor(t *testing.T) {
	const file = "11111111-1111-4111-8111-111111111111"
	f := entity.Field{Name: "shape", Type: entity.TypeString, Widget: "tiles", Choices: &entity.ChoiceSet{Cardinality: "one", Empty: entity.ChoiceEmpty{Mode: "allow"}, Options: []entity.ChoiceOption{{ID: "round", Label: "Round", Picture: &entity.ChoicePicture{Kind: "file", Value: file}}}}}
	r := withCommands(resource.Command{Verb: "choose", Label: "Choose", Base: "/fixture", Fields: []entity.Field{f}})
	o := opts
	o.PictureSource = "/pictures/{id}"
	html := render(t, resource.Detail(r, o, map[string]any{"id": "one", "title": "Fixture"}, true).Body)
	if !strings.Contains(html, `src="/pictures/`+file+`"`) {
		t.Fatalf("command omitted picture: %s", html)
	}
}
