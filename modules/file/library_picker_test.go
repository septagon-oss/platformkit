package file_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func TestLibraryOffersAFileSelectionFromAnImageField(t *testing.T) {
	router, _, conn, _, manifest := libraryFixture(t)
	id, _ := uploadFrame(t, router, files)
	encodeNow(t, manifest, conn)
	panel := libraryRequest(t, router, http.MethodGet, libraryAddress+"?file="+id, host, "", true)
	stamp := regexp.MustCompile(`name="updated" value="([^"]+)"`).FindStringSubmatch(panel.Body.String())
	if len(stamp) != 2 {
		t.Fatal("the image panel has no revision stamp")
	}
	caption := libraryRequest(t, router, http.MethodPost, libraryAddress+"/"+id+"/alt", host,
		url.Values{"updated": {stamp[1]}, "alt": {"A captioned image"}}.Encode(), true)
	if caption.Code != http.StatusSeeOther {
		t.Fatalf("captioning the pickable image = %d", caption.Code)
	}
	at := libraryAddress + "/pick?" + url.Values{"returns": {"/app/content/contents/new"}, "field": {"image"}}.Encode()
	rec := libraryRequest(t, router, http.MethodGet, at, host, "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d; want a picker a field can open", at, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Use image") || !strings.Contains(rec.Body.String(), id) {
		t.Error("the picker offers no Use image action with the eligible file's id")
	}
}

func TestLibraryLetsAMemberUploadImages(t *testing.T) {
	router, _, _, _, _ := libraryFixture(t)
	rec := libraryRequest(t, router, http.MethodGet, libraryAddress, host, "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("library = %d", rec.Code)
	}
	if !regexp.MustCompile(`<input[^>]*type="file"`).MatchString(rec.Body.String()) {
		t.Error("the empty library has no file input; a member cannot begin the upload-and-pick journey")
	}
}
