package admin_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/modules/admin"
	billing "github.com/septagon-oss/platformkit/modules/billing"
	billingcontracts "github.com/septagon-oss/platformkit/modules/billing/contracts"
	site "github.com/septagon-oss/platformkit/modules/site"
	sitecontracts "github.com/septagon-oss/platformkit/modules/site/contracts"
	user "github.com/septagon-oss/platformkit/modules/user"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/ui/resource"
	g "maragu.dev/gomponents"
)

// Which modules the reference app generates screens for is a short list: the four
// rest.Spec's it composes — people, tasks, content and plans — plus the site's one
// settings row. Two of them carried their own words; the rest answered a Portuguese
// reader with the English in their structs' doc comments, because a schema's field
// name, note and enum value are only copy in the language the module was written in.
//
// The case asks both halves of the cure. A Portuguese page must not keep an English
// source string, and an English page must say *exactly* the string it said before its
// module had a catalogue — the key exists to translate a page, not to re-word one that
// nobody asked to change. That second half is why the same control is drawn twice:
// once through the catalogues and once with no locale at all, which is the renderer's
// own fallback, the schema's English.
func TestEveryGeneratedScreenSpeaksTheRequestsLanguage(t *testing.T) {
	catalog := xtext.Load("en", admin.Catalogue(), user.Catalogue(), site.Catalogue(), billing.Catalogue(),
		xtext.Source{Name: "resource", FS: resource.Catalogues()})
	pt, en := catalog.Select("pt-PT"), catalog.Select("en")
	// The six doors over a person's own record. Their Summary and Description are
	// what the API declares, and the buttons on the detail page repeat them.
	commands := []resource.Command{
		{Verb: "approve-registration", Label: "Approve a pending registration",
			Description: "Activates a pending account without changing its password or roles. Active accounts are unchanged; invitations and deactivated accounts cannot be approved.",
			Base:        "/app/user/users"},
		{Verb: "set-password", Label: "Set a user's password",
			Description: "Stores an argon2id hash and makes an invited user active. It is not idempotent: setting a password to what it already was is still a password change, and somebody who did it deliberately has to see it in their own audit trail.",
			Base:        "/app/user/users"},
		{Verb: "roles", Label: "Set a user's roles",
			Description: "Replaces the roles this person holds. The same set again, in any order, changes nothing and publishes nothing.",
			Base:        "/app/user/users"},
		{Verb: "handle", Label: "Claim or change a user's handle",
			Description: "The lower-case name this person answers to in this tenant, used in URLs and mentions. Renameable, because every key and audit entry names the uuid and nothing has to follow the name: the previous holder stays in user.handle_set. A handle somebody else holds is a conflict that names the rule and not the holder, and a handle cannot be released.",
			Base:        "/app/user/users"},
		{Verb: "deactivate", Label: "Deactivate a user",
			Description: "Stops the person signing in. Their sessions stop working with them, because a session is only honoured for an active user, so there is no list of sessions to walk. Deactivating them again changes nothing.",
			Base:        "/app/user/users"},
		{Verb: "language", Label: "Choose which language this person is answered in",
			Description: "Writes the signed-in person's own language, which beats browser negotiation on every page they are shown and survives signing out and back in. An empty locale takes the choice back and leaves it to the browser and the tenant's default. A tag this tenant is not served in is refused: the set is the tenant's declaration and a choice outside it would be a preference that silently means nothing.",
			Base:        "/app/user/users"},
	}
	// The words as the page must not say them: the label, the note under a control,
	// a choice in a select, a command button and its accessible name. Where the
	// renderer escapes one (&#39; for an apostrophe) is the page's doing, and the
	// expectation is written the way the page comes out.
	cases := []struct {
		module, entityName string
		typ                reflect.Type
		words              []string
	}{
		{"user", "user", reflect.TypeFor[usercontracts.User](), []string{
			"Address this person signs in with, unique within the tenant",
			"Lower-case name this person answers to in this tenant, empty until claimed",
			"Language this person chose, empty while the browser and the tenant decide",
			">Lifecycle state<", "Roles this person holds in this tenant",
			">Invited<", ">Pending<", ">Unverified<", ">Active<", ">Inactive<",
		}},
		{"site", "settings", reflect.TypeFor[sitecontracts.SiteSettings](), []string{
			">Title<", "The site&#39;s name", ">Tagline<", "The line under the name",
			">Home slug<", "Slug of the content served at /",
			">Theme<", "Colour scheme, or system to follow the visitor&#39;s",
			">Primary color<", "Brand colour, #rrggbb", ">Logo file id<", "File id of the logo",
			">Light<", ">Dark<", ">System<",
		}},
		{"billing", "plan", reflect.TypeFor[billingcontracts.Plan](), []string{
			"Stable identifier, unique within the tenant", ">Price cents<",
			"Price of one interval, in the currency&#39;s minor unit",
			">Currency<", "ISO 4217 code", ">Interval<", "How long one period lasts",
			"Feature names this plan includes", ">Active<",
			"Whether this plan accepts new subscriptions", ">Month<", ">Year<",
		}},
	}
	for _, c := range cases {
		draw := func(t *testing.T, loc *locale.Locale, kind string) string {
			t.Helper()
			r := resource.Resource{
				Schema: entity.Schema{Module: c.module, Entity: c.entityName, Fields: crud.FieldsOf(c.typ)},
				Name:   entity.Name{One: c.entityName, Other: c.entityName + "s"},
				Screen: "/app/" + c.module + "/" + c.entityName,
			}
			if c.module == "user" && kind == "detail" {
				r.Commands = commands
			}
			var body []g.Node
			if kind == "form" {
				body = resource.Form(r, resource.Options{Locale: loc}, r.Screen, "New", nil, nil, "", true).Body
			} else {
				body = resource.Detail(r, resource.Options{Locale: loc}, nil, true).Body
			}
			var out strings.Builder
			if err := g.Group(body).Render(&out); err != nil {
				t.Fatal(err)
			}
			return out.String()
		}
		fallback, english := draw(t, nil, "form"), draw(t, &en, "form")
		page := draw(t, &pt, "form")
		for _, word := range c.words {
			if !strings.Contains(fallback, word) {
				t.Errorf("%s form never fell back to %q, so this case asks nothing of it", c.module, word)
			}
			if !strings.Contains(english, word) {
				t.Errorf("%s form in English no longer says the schema's own %q", c.module, word)
			}
			if strings.Contains(page, word) {
				t.Errorf("Portuguese %s form retains English interface copy %q", c.module, word)
			}
		}
		if c.module != "user" {
			continue
		}
		// The commands are the same argument in a different place on the page:
		// their English is declared in Go beside the route, and a control that
		// printed it as declared would answer a Portuguese reader in the language
		// the API was written in.
		bare, english, page := draw(t, nil, "detail"), draw(t, &en, "detail"), draw(t, &pt, "detail")
		for _, word := range []string{
			"Approve a pending registration", "Set a user&#39;s password", "Set a user&#39;s roles",
			"Claim or change a user&#39;s handle", "Deactivate a user",
			"Choose which language this person is answered in", "Activates a pending account",
			"Stores an argon2id hash", "Replaces the roles this person holds",
			"The lower-case name this person answers to", "Stops the person signing in",
			"Writes the signed-in person&#39;s own language",
		} {
			if !strings.Contains(bare, word) || !strings.Contains(english, word) {
				t.Errorf("user detail lost the declared English %q (without a catalog: %v, in English: %v)",
					word, strings.Contains(bare, word), strings.Contains(english, word))
			}
			if strings.Contains(page, word) {
				t.Errorf("Portuguese user detail retains English command copy %q", word)
			}
		}
		for _, word := range []string{
			"Aprovar uma inscrição pendente", "Definir a palavra-passe de uma pessoa",
			"Desativar uma pessoa", "Escolher o idioma em que esta pessoa é respondida",
			"Ativa uma conta pendente",
		} {
			if !strings.Contains(page, word) {
				t.Errorf("Portuguese user detail does not say %q", word)
			}
		}
	}
}
