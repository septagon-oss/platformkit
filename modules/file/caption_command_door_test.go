package file_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// A catalog client performs a row command as POST {path}/{id}/{verb}, or at the
// path the catalog prints when that derivation is untrue. The caption the catalog
// offers has to answer at that address with that method.
func TestTheCatalogsCaptionCommandAnswersWhereTheCatalogSendsIt(t *testing.T) {
	router, admin, _, _, _, api := libraryFixtureAPI(t)
	id, _ := uploadFrame(t, router, files)
	var updated string
	if err := admin.QueryRowContext(t.Context(),
		`SELECT to_char(updated_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') FROM files WHERE id=$1`, id).Scan(&updated); err != nil {
		t.Fatal(err)
	}
	ctx := tenancy.WithPrincipal(tenancy.WithTenant(context.Background(), acme), tenancy.Principal{UserID: uuid.New()})
	catalog := screens.Describe(ctx, api.Resources())
	at := ""
	for _, entry := range catalog.Resources {
		if entry.Module != "file" {
			continue
		}
		for _, command := range entry.Commands {
			if command.Verb != "alt" {
				continue
			}
			at = entry.Path + "/" + id + "/alt"
			if command.Path != "" {
				at = strings.ReplaceAll(command.Path, "{id}", id)
			}
		}
	}
	if at == "" {
		t.Fatal("the catalog offers no caption command on file")
	}
	req := httptest.NewRequest(http.MethodPost, "http://"+host+at,
		strings.NewReader(`{"alt":"A red kite over the ridge","decorative":false,"expectedUpdatedAt":"`+updated+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	var alt string
	if err := admin.QueryRowContext(t.Context(), "SELECT alt FROM files WHERE id=$1", id).Scan(&alt); err != nil {
		t.Fatal(err)
	}
	if rec.Code >= 300 || alt != "A red kite over the ridge" {
		t.Errorf("POST %s (the catalog's caption door) = %d %s; alt=%q", at, rec.Code, rec.Body.String(), alt)
	}
}
