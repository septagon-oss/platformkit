package file_test

import (
	"net/http"
	"net/url"
	"regexp"
	"testing"
	"time"
)

func TestLibraryRejectsAnotherTenantsReadAndCaption(t *testing.T) {
	router, admin, _, _, _ := libraryFixture(t)
	code, body := upload(t, router, files, "private.txt", "text/plain", "private bytes")
	if code != http.StatusCreated {
		t.Fatalf("upload = %d %s", code, body)
	}
	id := regexp.MustCompile(`"id":"([^"]+)"`).FindStringSubmatch(body)[1]
	for _, at := range []string{libraryAddress + "?file=" + id, files + "/" + id} {
		rec := libraryRequest(t, router, http.MethodGet, at, otherHost, "", true)
		if rec.Code != http.StatusNotFound {
			t.Errorf("other tenant GET %s = %d", at, rec.Code)
		}
	}
	var before int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox").Scan(&before); err != nil {
		t.Fatal(err)
	}
	rec := libraryRequest(t, router, http.MethodPost, libraryAddress+"/"+id+"/alt", otherHost,
		url.Values{"alt": {"changed"}, "updated": {time.Now().UTC().Format(time.RFC3339Nano)}}.Encode(), true)
	if rec.Code != http.StatusNotFound {
		t.Errorf("other tenant caption = %d", rec.Code)
	}
	var after int
	var alt string
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRowContext(t.Context(), "SELECT alt FROM files WHERE id=$1", id).Scan(&alt); err != nil {
		t.Fatal(err)
	}
	if after != before || alt != "" {
		t.Errorf("cross-tenant refusal left alt=%q, outbox=%d (before %d)", alt, after, before)
	}
}

func TestLibraryFiltersRefuseInvalidInputsAsValidation(t *testing.T) {
	router, _, _, _, _ := libraryFixture(t)
	for _, query := range []string{"type=pictures", "sort=pixels"} {
		rec := libraryRequest(t, router, http.MethodGet, libraryAddress+"?"+query, host, "", true)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("GET library with %s = %d; want a correctable 422 validation refusal", query, rec.Code)
		}
	}
}
