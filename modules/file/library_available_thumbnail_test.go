package file_test

import (
	"bytes"
	"encoding/json"
	"html"
	"image"
	"image/jpeg"
	"net/http"
	"regexp"
	"strconv"
	"testing"
)

func TestLibraryThumbnailsNameAvailableImages(t *testing.T) {
	for _, width := range []int{320, 1000} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			router, _, conn, _, manifest := libraryFixture(t)
			var picture bytes.Buffer
			if err := jpeg.Encode(&picture, image.NewRGBA(image.Rect(0, 0, width, 200)), nil); err != nil {
				t.Fatal(err)
			}
			code, body := upload(t, router, files, "image.jpg", "image/jpeg", picture.String())
			if code != http.StatusCreated {
				t.Fatalf("upload = %d %s", code, body)
			}
			var row struct{ ID string }
			if err := json.Unmarshal([]byte(body), &row); err != nil || row.ID == "" {
				t.Fatalf("read uploaded file: %v, %s", err, body)
			}
			check := func(stage string) {
				page := libraryRequest(t, router, http.MethodGet, libraryAddress, host, "", true)
				if page.Code != http.StatusOK {
					t.Fatalf("library = %d", page.Code)
				}
				source := regexp.MustCompile(`<img[^>]*src="([^"]+` + row.ID + `[^"]*)"`).FindStringSubmatch(page.Body.String())
				if len(source) != 2 {
					t.Fatal("the library draws no thumbnail for the uploaded file")
				}
				at := html.UnescapeString(source[1])
				thumbnail := libraryRequest(t, router, http.MethodGet, at, host, "", true)
				if thumbnail.Code != http.StatusOK {
					t.Errorf("%s: %dpx image thumbnail GET %s = %d; want a served image", stage, width, at, thumbnail.Code)
					return
				}
				if _, _, err := image.DecodeConfig(bytes.NewReader(thumbnail.Body.Bytes())); err != nil {
					t.Errorf("%s: thumbnail is not an image: %v", stage, err)
				}
			}
			check("before encoding")
			encodeNow(t, manifest, conn)
			check("after encoding")
		})
	}
}
