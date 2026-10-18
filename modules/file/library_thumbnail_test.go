package file_test

import (
	"bytes"
	"image"
	"image/jpeg"
	"net/http"
	"regexp"
	"testing"
)

func TestLibraryThumbnailServesThePlannedWidth(t *testing.T) {
	router, _, conn, _, manifest := libraryFixture(t)
	id, _ := uploadFrame(t, router, files)
	encodeNow(t, manifest, conn)
	rec := libraryRequest(t, router, http.MethodGet, libraryAddress, host, "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("library = %d: %s", rec.Code, rec.Body.String())
	}
	match := regexp.MustCompile(`<img[^>]*src="([^"]+` + id + `[^"]*)"`).FindStringSubmatch(rec.Body.String())
	if len(match) != 2 {
		t.Fatalf("the library renders no image for %s", id)
	}
	thumb := libraryRequest(t, router, http.MethodGet, match[1], host, "", true)
	if thumb.Code != http.StatusOK {
		t.Fatalf("thumbnail %s = %d", match[1], thumb.Code)
	}
	frame, _, err := image.DecodeConfig(bytes.NewReader(thumb.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if frame.Width != 480 {
		t.Errorf("library thumbnail %s decoded as %dx%d; want the planned 480px width", match[1], frame.Width, frame.Height)
	}
	control := libraryRequest(t, router, http.MethodGet, files+"/"+id+"/content?w=480", host, "", true)
	frame, err = jpeg.DecodeConfig(bytes.NewReader(control.Body.Bytes()))
	if err != nil || frame.Width != 480 {
		t.Fatalf("the known width door cannot serve the fixture: %+v, %v", frame, err)
	}
}
