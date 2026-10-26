package tenanttest

import (
	"errors"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

func setLocale(t *testing.T, f Fixture) {
	created, err := f.Service.Create(f.Ctx, f.Tx, acme())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// What a tenant nobody chose a language for is served in, read the way a
	// request reads it: exactly its default, and nothing else. The migration
	// gives a tenant that predates the column the same single row, and this
	// command is the only thing that adds a language, so a create that wrote
	// a wider set would leave two identical tenants answering one browser in
	// two different languages.
	born, err := f.Service.ByHost(f.Ctx, f.Tx, "acme.example.com")
	if err != nil {
		t.Fatalf("ByHost: %v", err)
	}
	if created.DefaultLocale == "" {
		t.Errorf("a new tenant named no default language, so every request to it falls back to nothing")
	}
	if len(created.Locales) != 0 {
		t.Errorf("a new tenant was born served in %v beside its default %q; SetLocale is the command that says so",
			created.Locales, created.DefaultLocale)
	}
	if born.Languages == nil || born.Languages.Default != created.DefaultLocale || len(born.Languages.Others) != 0 {
		t.Errorf("resolving the tenant's own host offers %v, want the default %q alone",
			born.Languages, created.DefaultLocale)
	}
	got, err := f.Service.SetLocale(f.Ctx, f.Tx, created.ID, contracts.SetLocale{
		Default:   "pt-PT",
		Supported: []string{"en", "PT-pt", "pt-PT"},
	})
	if err != nil {
		t.Fatalf("SetLocale: %v", err)
	}
	if got.DefaultLocale != "pt-PT" {
		t.Errorf("default is %q, want pt-PT as a tag spells it", got.DefaultLocale)
	}
	// The set is sorted, deduplicated, and does not repeat the default: the
	// case is normalised, so "PT-pt" is the same language as "pt-PT".
	if want := []string{"en"}; !slices.Equal(got.Locales, want) {
		t.Errorf("the set behind the default is %v, want %v", got.Locales, want)
	}
	resolved, err := f.Service.ByHost(f.Ctx, f.Tx, "acme.example.com")
	if err != nil {
		t.Fatalf("ByHost: %v", err)
	}
	// What the loader answers is what a page negotiates over, so this is the
	// assertion that the tenant's own default is first: a list whose head is
	// somebody else is a tenant answered in the wrong language.
	if resolved.Languages == nil || resolved.Languages.Default != "pt-PT" ||
		!slices.Equal(resolved.Languages.Others, []string{"en"}) {
		t.Errorf("the loader carried %v, want pt-PT first with en behind it", resolved.Languages)
	}
	published(t, f, audited(contracts.EventCreated, contracts.EventLocaleSet)...)
}

func repeatLocale(t *testing.T, f Fixture) {
	created, err := f.Service.Create(f.Ctx, f.Tx, acme())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	in := contracts.SetLocale{Default: "pt-PT", Supported: []string{"en"}}
	if _, err := f.Service.SetLocale(f.Ctx, f.Tx, created.ID, in); err != nil {
		t.Fatalf("SetLocale: %v", err)
	}
	if _, err := f.Service.SetLocale(f.Ctx, f.Tx, created.ID, in); err != nil {
		t.Fatalf("SetLocale again: %v", err)
	}
	published(t, f, audited(contracts.EventCreated, contracts.EventLocaleSet)...)
}

func refuseInvalidLanguage(t *testing.T, f Fixture) {
	created, err := f.Service.Create(f.Ctx, f.Tx, acme())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	for _, in := range []contracts.SetLocale{
		{Default: "portuguese", Supported: []string{"en"}},
		{Default: "pt-PT", Supported: []string{"english"}},
		{Default: " ", Supported: nil},
	} {
		if _, err := f.Service.SetLocale(f.Ctx, f.Tx, created.ID, in); !errors.Is(err, crud.ErrInvalid) {
			t.Errorf("SetLocale(%q, %v) = %v, want ErrInvalid", in.Default, in.Supported, err)
		}
	}
	// Nothing was written by the refused calls: the tenant is still served in
	// the language it was created with, and said nothing about it.
	after, err := f.Service.Get(f.Ctx, f.Tx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if after.DefaultLocale != created.DefaultLocale || len(after.Locales) != 0 {
		t.Errorf("a refused SetLocale left %q with %v", after.DefaultLocale, after.Locales)
	}
	published(t, f, audited(contracts.EventCreated)...)
}

func refuseUnavailableLanguage(t *testing.T, f Fixture) {
	created, err := f.Service.Create(f.Ctx, f.Tx, acme())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// The installation names the languages its catalogues answer in, and a
	// tenant cannot be served in one nobody wrote: the page would declare that
	// language and show the source copy. InstallationLanguages is en and pt-PT
	// for both implementations, so German is the one with no copy behind it.
	for _, in := range []contracts.SetLocale{
		{Default: "de-DE", Supported: nil},
		{Default: "pt-PT", Supported: []string{"de-DE"}},
	} {
		if _, err := f.Service.SetLocale(f.Ctx, f.Tx, created.ID, in); !errors.Is(err, crud.ErrInvalid) {
			t.Errorf("SetLocale(%q, %v) = %v, want ErrInvalid: the installation has no copy in it",
				in.Default, in.Supported, err)
		}
	}
	after, err := f.Service.Get(f.Ctx, f.Tx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if after.DefaultLocale != created.DefaultLocale || len(after.Locales) != 0 {
		t.Errorf("a refused SetLocale left %q served in %v", after.DefaultLocale, after.Locales)
	}
	published(t, f, audited(contracts.EventCreated)...)
}

// refuseSupersededLocale holds an operator declaration to the revision its page
// read: the counter is the tenant's, not the door's, so winning the row here would
// erase the declaration that moved it.
func refuseSupersededLocale(t *testing.T, f Fixture) {
	created, err := f.Service.Create(f.Ctx, f.Tx, acme())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// The counter is the tenant's, not the door's: one declaration lands and
	// moves it, and the page that read the set before that landed has a number
	// the row no longer holds.
	born := created.LocaleRevision
	if _, err := f.Service.SetLocale(f.Ctx, f.Tx, created.ID, contracts.SetLocale{
		Default: "pt-PT", Supported: []string{"en"},
	}); err != nil {
		t.Fatalf("SetLocale: %v", err)
	}
	stale := contracts.LocaleRevision(born)
	row, err := f.Service.SetLocale(f.Ctx, f.Tx, created.ID, contracts.SetLocale{
		Default: "en", Supported: []string{"pt-PT"}, ExpectedRevision: &stale,
	})
	if !errors.Is(err, crud.ErrConflict) {
		t.Errorf("a declaration read at revision %d returned %v, want a conflict", born, err)
	}
	if row != nil {
		t.Error("a refused declaration returned a row, which is how a stale page reads as a saved one")
	}
	// And nothing moved: the set, its count and the trail are the accepted
	// declaration's, not the refused one's.
	after, err := f.Service.Get(f.Ctx, f.Tx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if after.DefaultLocale != "pt-PT" || !slices.Equal(after.Locales, []string{"en"}) || after.LocaleRevision != born+1 {
		t.Errorf("the refused declaration left %q %v at revision %d, want pt-PT [en] at %d",
			after.DefaultLocale, after.Locales, after.LocaleRevision, born+1)
	}
	published(t, f, audited(contracts.EventCreated, contracts.EventLocaleSet)...)
}
