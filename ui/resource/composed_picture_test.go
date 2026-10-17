package resource_test

import (
	g "maragu.dev/gomponents"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/forms"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func TestComposedPictureListUsesItsOwnValidatedDoor(t *testing.T) {
	token, _ := entity.ComposedPicture("fixture /? # key")
	fields := []entity.Field{{Name: "picture", Type: entity.TypePicture, ReadOnly: true, Present: "image"}, {Name: "title", Type: entity.TypeString}}
	res := resource.Resource{Screen: "/app/fixture/pictures", Schema: entity.Schema{Module: "fixture", Entity: "picture", Fields: fields}}
	options := opts
	options.PictureSource = "/files/{id}"
	options.ComposedSource = "/api/v1/picture/pictures/{reference}/content"
	draw := func(p any, options resource.Options) string {
		return render(t, resource.List(res, options, []map[string]any{{"id": "one", "title": "Geometry", "picture": p}}, 1, 1, "", false).Body)
	}
	markup := draw(token, options)
	if !strings.Contains(markup, `src="/api/v1/picture/pictures/`+string(token)+`/content"`) || !strings.Contains(markup, "<img") || strings.Contains(markup, "<svg") || strings.Contains(markup, ">"+string(token)+"<") {
		t.Fatalf("image list markup %s", markup)
	}
	if strings.Contains(markup, "sort=picture") {
		t.Fatal("picture sort offered")
	}
	for _, value := range []any{entity.Picture("c1.eB"), entity.Picture(""), nil} {
		if body := draw(value, options); strings.Contains(body, "<img") || strings.Contains(body, "/files/") {
			t.Fatalf("invalid/empty picture fetched %s", body)
		}
	}
	options.ComposedSource = ""
	if body := draw(token, options); strings.Contains(body, "<img") || strings.Contains(body, string(token)) {
		t.Fatalf("missing door leaked token or fallback %s", body)
	}
	for _, create := range []bool{true, false} {
		view, err := forms.Example("picture", forms.Model{Fields: []forms.Field{{Definition: fields[0]}}, Values: map[string]string{"picture": string(token)}, Immutable: []string{"picture"}, Create: create}, forms.Options{Namespace: "picture-form"})
		if err != nil {
			t.Fatal(err)
		}
		body := render(t, []g.Node{view.Node})
		if strings.Contains(body, string(token)) || strings.Contains(body, `name="picture"`) {
			t.Fatalf("picture form control %s", body)
		}
	}
}
