package file_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

func TestLibraryAnchorsFollowTheSelectedFileAndManagementGrant(t *testing.T) {
	router, _, _, _, _ := libraryFixture(t)
	id, _ := uploadFrame(t, router, files)
	at := libraryAddress + "?file=" + id + "&q=sample"
	response := libraryRequest(t, router, http.MethodGet, at, host, "", true)
	if response.Code != http.StatusOK {
		t.Fatalf("library = %d", response.Code)
	}
	view := contracts.Views(true, true, true)[0]
	for _, anchor := range view.Anchors() {
		ref := `data-screen-anchor="` + string(view.Ref()) + "#" + anchor.Name() + `"`
		if count := strings.Count(response.Body.String(), ref); count != 1 {
			t.Errorf("%s count = %d", anchor.Name(), count)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "http://"+host+at, nil)
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "read-only"})
	read := httptest.NewRecorder()
	router.ServeHTTP(read, req)
	if read.Code != http.StatusOK {
		t.Fatalf("read-only library = %d", read.Code)
	}
	for _, name := range []string{"field.file", "action.upload", "field.alt", "field.decorative", "action.save-alt", "action.delete"} {
		if strings.Contains(read.Body.String(), `data-screen-anchor="`+string(view.Ref())+"#"+name+`"`) {
			t.Errorf("read-only response contains %s", name)
		}
	}
}
