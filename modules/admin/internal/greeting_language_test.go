package internal

import (
	"errors"
	"os"
	"testing"

	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/page"
)

func TestAnUnnamedCallerIsGreetedInTheRequestsLanguage(t *testing.T) {
	catalog := xtext.Load("en", xtext.Source{Name: "admin", FS: os.DirFS("../messages")})
	for _, language := range []struct{ tag, fallback string }{{"en", "Signed in"}, {"pt-PT", "Sessão iniciada"}} {
		for _, tc := range []struct {
			name   string
			people People
			want   string
		}{
			{"missing provider", nil, language.fallback},
			{"missing row", peopleDouble{}, language.fallback},
			{"failed read", peopleDouble{err: errors.New("unavailable")}, language.fallback},
			{"blank name and address", peopleDouble{person: Person{DisplayName: "  ", Email: "\t"}}, language.fallback},
			{"named person", peopleDouble{person: Person{DisplayName: "Ada"}}, "Ada"},
			{"address only", peopleDouble{person: Person{Email: "ada@example.test"}}, "ada@example.test"},
		} {
			t.Run(language.tag+"/"+tc.name, func(t *testing.T) {
				selected := catalog.Select(language.tag)
				r := page.Request{Tenant: tenancy.Tenant{Name: "End to end"}, Path: "/app", Host: "app.acme.test",
					SignedIn: true, Principal: tenancy.Principal{UserID: theID}, Locale: &selected}
				markup := render(t, chromeTestFrame(t, tc.people)(t.Context(), r, nil))
				if got := callerLine(t, markup); got != tc.want {
					t.Errorf("caller line = %q, want %q", got, tc.want)
				}
			})
		}
	}
}
