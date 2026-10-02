package main

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

func TestTheAccountDefaultOptionSpeaksTheChosenLanguage(t *testing.T) {
	cfg, admin := languageApp(t, `{"default":"en","supported":["pt-PT"]}`)
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost,
		"/api/v1/user/users/language", `{"locale":"pt-PT"}`)
	if code != http.StatusOK {
		// The versioned account command may replace the old route. Reach the page
		// through either command without relying on the English option under test.
		const path = "/api/v1/user/me/language"
		readCode, readBody := do(t, cfg, admin, http.MethodGet, acmeHost, path, "")
		if readCode != http.StatusOK {
			t.Fatalf("read language before choosing Portuguese = %d: %s (old route %d: %s)",
				readCode, readBody, code, body)
		}
		var state struct {
			Revision int64 `json:"revision"`
		}
		if err := json.Unmarshal([]byte(readBody), &state); err != nil {
			t.Fatal(err)
		}
		put := fmt.Sprintf(`{"language":"pt-PT","expectedRevision":%d}`, state.Revision)
		if code, body = do(t, cfg, admin, http.MethodPut, acmeHost, path, put); code < 200 || code >= 300 {
			t.Fatalf("choose Portuguese = %d: %s", code, body)
		}
	}
	page := dashboard(t, cfg, admin, "en")
	if got := pageLang(page); got != "pt-PT" {
		t.Fatalf("the chosen-language page declares %q, want pt-PT", got)
	}
	option := regexp.MustCompile(`(?s)<select[^>]*name="locale"[^>]*>.*?<option[^>]*value=""[^>]*>([^<]*)</option>`).
		FindStringSubmatch(page)
	if option == nil {
		t.Fatal("the account language control has no empty option to restore the tenant default")
	}
	want := catalogues().Select("pt-PT").Text("admin.account.unset", "Use tenant default")
	if got := strings.TrimSpace(html.UnescapeString(option[1])); got == "Choose…" || got != want {
		t.Errorf("the selectable default option says %q on a Portuguese page, want %q", got, want)
	}
}
