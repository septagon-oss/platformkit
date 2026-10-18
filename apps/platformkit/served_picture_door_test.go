package main

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	filecontracts "github.com/septagon-oss/platformkit/modules/file/contracts"
)

// TestTheServedCatalogueNamesThePictureDoorAndTheCover asks the running reference
// composition, as a signed-in member, for the workspace catalogue a phone reads:
// it names the file module's door and its widths once, and the content entry says
// which field its record opens on and which one its rows show.
func TestTheServedCatalogueNamesThePictureDoorAndTheCover(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)
	who := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	code, body := do(t, cfg, who, http.MethodGet, acmeHost, "/api/v1/app/resources", "")
	if code != http.StatusOK {
		t.Fatalf("GET /api/v1/app/resources = %d %s", code, body)
	}
	var catalog struct {
		Pictures *struct {
			Source string `json:"source"`
			Widths []int  `json:"widths"`
		} `json:"pictures"`
		Resources []struct {
			Module, Entity string
			Fields         []struct{ Name, Present string }
			Presentation   struct{ CoverField, ThumbnailField string }
		} `json:"resources"`
	}
	if err := json.Unmarshal([]byte(body), &catalog); err != nil {
		t.Fatal(err)
	}
	if catalog.Pictures == nil || catalog.Pictures.Source != filecontracts.PictureSource ||
		!slices.Equal(catalog.Pictures.Widths, filecontracts.VariantWidthSet) {
		t.Errorf("pictures = %+v, want source %q and widths %v", catalog.Pictures, filecontracts.PictureSource, filecontracts.VariantWidthSet)
	}
	found := false
	for _, r := range catalog.Resources {
		if r.Module != "content" || r.Entity != "content" {
			continue
		}
		found = true
		if r.Presentation.CoverField != "coverImageId" || r.Presentation.ThumbnailField != "coverImageId" {
			t.Errorf("content presentation = %+v, want coverImageId as cover and thumbnail", r.Presentation)
		}
		present := ""
		for _, f := range r.Fields {
			if f.Name == "coverImageId" {
				present = f.Present
			}
		}
		if present != "image" {
			t.Errorf("content coverImageId is presented %q, want image", present)
		}
	}
	if !found {
		t.Error("the served catalogue lists no content resource")
	}
}
