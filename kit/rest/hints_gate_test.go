package rest_test

// hints_gate_test.go is the mount gate for the reading contract: every
// declaration that could only ever draw the wrong screen refuses at Mount, with
// a message naming the offender and the list that refused it.
//
// The assertion matches the *message*, not merely that something was recovered:
// Mount on an httpx.Surfaces with no router behind it panics for its own reasons,
// so a bare recover() is satisfied by the wrong accident. See
// presentation_gate_test.go, which this file copies on purpose.

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
)

// The entities below differ only in the hint they carry, because the fault under
// test has to be the only thing about the mount that is wrong.

type hintedOK struct {
	crud.Base
	Title string `json:"title" ui:"label:Subject;visibility:shown"`
	Rank  int    `json:"rank"`
}

func (hintedOK) TableName() string { return "rest_hinted" }

type badFormat struct {
	crud.Base
	When string `json:"when" ui:"format:at-the-time"`
}

func (badFormat) TableName() string { return "rest_hinted" }

type badTone struct {
	crud.Base
	Level string `json:"level" enum:"low,high" enumTones:"low=brand"`
}

func (badTone) TableName() string { return "rest_hinted" }

type emptyTone struct {
	crud.Base
	Level string `json:"level" enum:"low,high" enumTones:"low="`
}

func (emptyTone) TableName() string { return "rest_hinted" }

type toneOffEnum struct {
	crud.Base
	Level string `json:"level" enum:"low,high" enumTones:"urgent=info"`
}

func (toneOffEnum) TableName() string { return "rest_hinted" }

type labelsNoEnum struct {
	crud.Base
	Name string `json:"name" enumLabels:"a=A"`
}

func (labelsNoEnum) TableName() string { return "rest_hinted" }

type moneyOnString struct {
	crud.Base
	Amount string `json:"amount" ui:"format:money;currency:unit;scale:2"`
	Unit   string `json:"unit"`
}

func (moneyOnString) TableName() string { return "rest_hinted" }

type moneyNoCurrency struct {
	crud.Base
	Amount int64 `json:"amount" ui:"format:money"`
}

func (moneyNoCurrency) TableName() string { return "rest_hinted" }

type scaleNoCurrency struct {
	crud.Base
	Amount int64 `json:"amount" ui:"format:money;scale:2"`
}

func (scaleNoCurrency) TableName() string { return "rest_hinted" }

type currencyNotMoney struct {
	crud.Base
	Amount int64  `json:"amount" ui:"currency:unit"`
	Unit   string `json:"unit"`
}

func (currencyNotMoney) TableName() string { return "rest_hinted" }

type currencyIsNotAString struct {
	crud.Base
	Amount int64 `json:"amount" ui:"format:money;currency:unit;scale:2"`
	Unit   int   `json:"unit"`
}

func (currencyIsNotAString) TableName() string { return "rest_hinted" }

type scaleTooWide struct {
	crud.Base
	Amount int64  `json:"amount" ui:"format:money;currency:unit;scale:8"`
	Unit   string `json:"unit"`
}

func (scaleTooWide) TableName() string { return "rest_hinted" }

type badReference struct {
	crud.Base
	Owner string `json:"owner" ui:"reference:user"`
}

func (badReference) TableName() string { return "rest_hinted" }

// shownAndHidden is the pair that resolves by precedence rather than by refusal:
// an explicit `visibility` is the author's word about the field's reading, so
// `shown` puts back on the list the column the older `hide:list` tag took off it.
// entity.Field.OnList is where that resolves, and ui/resource renders it.
type shownAndHidden struct {
	crud.Base
	Source string `json:"source" ui:"visibility:shown;hide:list"`
}

func (shownAndHidden) TableName() string { return "rest_hinted" }

// hiddenBesideHideList is the pair that is *not* a refusal: hidden is the
// stronger declaration and implies off-list, so the older tag turns out to have
// been saying the same thing, and refusing it would make every entity that later
// says `hidden` about a field it already hid a two-file edit.
type hiddenBesideHideList struct {
	crud.Base
	Source string `json:"source" ui:"visibility:hidden;hide:list"`
}

func (hiddenBesideHideList) TableName() string { return "rest_hinted" }

type unknownSection struct {
	crud.Base
	DueAt string `json:"dueAt" ui:"section:timing"`
}

func (unknownSection) TableName() string { return "rest_hinted" }

func hintRefused(t *testing.T, mount func(), want ...string) {
	t.Helper()
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		mount()
	}()
	if recovered == nil {
		t.Fatalf("Mount accepted a declaration that must refuse: wanted %v", want)
	}
	message, _ := recovered.(string)
	for _, needle := range want {
		if !strings.Contains(message, needle) {
			t.Errorf("Mount refused for another reason: %v (want %q)", recovered, needle)
		}
	}
}

func mountSpec[T crud.Entity](p entity.EntryHints, path string) func() {
	return func() {
		rest.Spec[T]{Module: "hinted", Entity: "hinted", Path: path,
			Read: "hinted:read", Write: "hinted:write", Present: p}.Mount(httpx.Surfaces{})
	}
}

// TestEachInvalidHintRefusesToMountNamingIt walks the validation table. Each
// case names the offender and the vocabulary or the entity that refused it, so
// an author is not left hunting for which of fifteen names was the wrong one.
func TestEachInvalidHintRefusesToMountNamingIt(t *testing.T) {
	// A hint that says nothing about a field, so the fault a case names is the
	// case's own and not one the fixture brought with it.
	plain := entity.EntryHints{Icon: "document"}
	good := entity.EntryHints{
		Icon: "document", Singular: "hint", Plural: "hints",
		Group:        &entity.ResourceGroup{Key: "practice", Label: "Practice"},
		Sections:     []entity.EntitySection{{Key: "record", Label: "Record information"}},
		PrimaryField: "title", Sortable: []string{"title"},
	}
	t.Run("a declaration that names what exists mounts", func(t *testing.T) {
		// Called, not built: a mount closure nobody runs refuses nothing. The real
		// harness is not decoration either — past the gate Mount writes its routes
		// onto the routers a real API hands out, and a zero httpx.Surfaces would
		// fall over on the nil router for a reason that has nothing to do with the
		// declaration. The two pairs of `visibility` beside `hide:list` mount in
		// TestHiddenBesideHideListMounts and in
		// TestExplicitShownVisibilityOverridesHideListAtMount.
		mountAs(t, rest.Spec[*hintedOK]{Module: "hinted", Entity: "hinted", Path: "/things",
			Read: "hinted:read", Write: "hinted:write", Present: good}, caller{})
	})
	for _, tc := range []struct {
		name  string
		mount func()
		want  []string
	}{
		{"icon", mountSpec[*hintedOK](entity.EntryHints{Icon: "widget"}, "/xx"), []string{`icon "widget"`, "entity.Icons"}},
		{"order", mountSpec[*hintedOK](entity.EntryHints{Order: -1}, "/xx"), []string{"order -1", "no opinion"}},
		{"group key", mountSpec[*hintedOK](entity.EntryHints{Group: &entity.ResourceGroup{Key: "Record information", Label: "R"}}, "/xx"), []string{`group key "Record information"`, "lower-case identifier"}},
		{"group label", mountSpec[*hintedOK](entity.EntryHints{Group: &entity.ResourceGroup{Key: "practice"}}, "/xx"), []string{`group "practice" declares no label`}},
		{"section label", mountSpec[*hintedOK](entity.EntryHints{Sections: []entity.EntitySection{{Key: "record"}}}, "/xx"), []string{`section "record" declares no label`}},
		{"duplicate section", mountSpec[*hintedOK](entity.EntryHints{Sections: []entity.EntitySection{{Key: "record", Label: "A"}, {Key: "record", Label: "B"}}}, "/xx"), []string{`section key "record" is declared twice`}},
		{"primaryField", mountSpec[*hintedOK](entity.EntryHints{PrimaryField: "titlee"}, "/xx"), []string{`primaryField "titlee"`, "not a field"}},
		{"primaryField on hidden", mountSpec[*hiddenBesideHideList](entity.EntryHints{PrimaryField: "source"}, "/xx"), []string{`primaryField "source"`, "hidden"}},
		{"statusField", mountSpec[*hintedOK](entity.EntryHints{StatusField: "state"}, "/xx"), []string{`statusField "state"`, "not a field"}},
		{"summaryFields repeats", mountSpec[*hintedOK](entity.EntryHints{PrimaryField: "title", SummaryFields: []string{"title"}}, "/xx"), []string{"summaryFields repeats primaryField"}},
		{"summaryFields empty", mountSpec[*hintedOK](entity.EntryHints{SummaryFields: []string{}}, "/xx"), []string{"summaryFields declares no field"}},
		{"sections empty", mountSpec[*hintedOK](entity.EntryHints{Sections: []entity.EntitySection{}}, "/xx"), []string{"sections declares no block"}},
		{"sortable off entity", mountSpec[*hintedOK](entity.EntryHints{Sortable: []string{"rankk"}}, "/xx"), []string{`sortable "rankk"`, "not a field"}},
		{"format", mountSpec[*badFormat](plain, "/xx"), []string{`formatted "at-the-time"`, "entity.Formats"}},
		{"tone", mountSpec[*badTone](plain, "/xx"), []string{`tone "brand"`, "entity.Tones"}},
		{"empty tone", mountSpec[*emptyTone](plain, "/xx"), []string{`tone ""`, "entity.Tones"}},
		{"tone off enum", mountSpec[*toneOffEnum](plain, "/xx"), []string{`for "urgent"`, "enum does not hold"}},
		{"labels without enum", mountSpec[*labelsNoEnum](plain, "/xx"), []string{"declares enum label values and has no enum"}},
		{"money on a string", mountSpec[*moneyOnString](plain, "/xx"), []string{`"amount" is formatted money and is string`}},
		{"money with no currency", mountSpec[*moneyNoCurrency](plain, "/xx"), []string{"names no currency field"}},
		{"scale with no currency", mountSpec[*scaleNoCurrency](plain, "/xx"), []string{"no currency field to read the code from"}},
		{"currency without money", mountSpec[*currencyNotMoney](plain, "/xx"), []string{"is not formatted money"}},
		{"currency is not a string", mountSpec[*currencyIsNotAString](plain, "/xx"), []string{`reads its currency from "unit"`, "not a string field"}},
		{"scale out of range", mountSpec[*scaleTooWide](plain, "/xx"), []string{"scale 8", "0 to 3"}},
		{"reference shape", mountSpec[*badReference](plain, "/xx"), []string{`reference "user"`, `module/entity`}},
		// No case for `visibility` beside `hide:list` in either direction: the pair
		// resolves by precedence at entity.Field.OnList, and the two mounts above
		// are the cases that say so.
		{"unknown section", mountSpec[*unknownSection](entity.EntryHints{Sections: []entity.EntitySection{{Key: "record", Label: "Record"}}}, "/xx"), []string{`names section "timing"`, "declares no section as"}},
	} {
		t.Run(tc.name, func(t *testing.T) { hintRefused(t, tc.mount, tc.want...) })
	}
}

// TestHiddenBesideHideListMounts is the half of V15 that does not refuse, and it
// is a case because the tempting implementation refuses it and would then make
// every entity that later says `hidden` about a field it already hid a mount
// failure nobody caused.
func TestHiddenBesideHideListMounts(t *testing.T) {
	mountAs(t, rest.Spec[*hiddenBesideHideList]{Module: "hinted", Entity: "hinted",
		Path: "/things", Read: "hinted:read", Write: "hinted:write"}, caller{})
}

// TestShownBesideHideListMountsTheColumnTheOlderTagTook is the pair that does not
// agree, and it does not refuse either. `visibility` names one field's reading on
// every screen and `hide:list` names one column of one screen, so the narrower
// declaration is the override: `shown` wins the column back. The gate and the
// renderer read that one rule — entity.Field.OnList — and the screen's half is
// ui/resource's TestExplicitShownVisibilityOverridesHideListInTheList.
func TestShownBesideHideListMountsTheColumnTheOlderTagTook(t *testing.T) {
	mountAs(t, rest.Spec[*shownAndHidden]{Module: "hinted", Entity: "hinted",
		Path: "/things", Read: "hinted:read", Write: "hinted:write"}, caller{})
}

// TestASystemCommandMountsAndAnIncoherentOneDoesNot is D6 and V19-V21: a command
// no person is offered is a declaration the mount accepts and the browser
// generator declines, and a command that says both "offer me first" and "never
// offer me" is a contradiction refused where it is written.
func TestASystemCommandMountsAndAnIncoherentOneDoesNot(t *testing.T) {
	api, _, _ := mounted(t)
	surfaces := api.Surfaces(spec.Module)
	rest.Command(surfaces, spec, "purge-drafts", "Purge abandoned drafts",
		"An operator's door.", nil,
		func(context.Context, db.Tx[db.Tenant], uuid.UUID, struct{}) (*Task, error) { return nil, nil },
		rest.CommandOptions{Present: entity.CommandHints{System: true}})

	for _, tc := range []struct {
		name string
		opts rest.CommandOptions
		want []string
		verb string
	}{
		{"primary and destructive", rest.CommandOptions{Present: entity.CommandHints{Primary: true, Destructive: true}},
			[]string{"both primary and destructive"}, "archive"},
		{"system and primary", rest.CommandOptions{Present: entity.CommandHints{System: true, Primary: true}},
			[]string{"system and offers something"}, "archive"},
		{"confirmation with no title", rest.CommandOptions{Present: entity.CommandHints{
			Confirmation: &entity.CommandConfirmation{Body: "x", ConfirmLabel: "y"}}},
			[]string{"declares a confirmation with no title"}, "archive"},
		{"a verb that is not a path segment", rest.CommandOptions{}, []string{"is not a path segment"}, "Check SLA"},
		{"a toast that is a page", rest.CommandOptions{Present: entity.CommandHints{
			SuccessMessage: strings.Repeat("long ", 100)}}, []string{"a toast is not a page"}, "archive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hintRefused(t, func() {
				rest.Command(surfaces, spec, tc.verb, "Archive", "d", nil,
					func(context.Context, db.Tx[db.Tenant], uuid.UUID, struct{}) (*Task, error) { return nil, nil },
					tc.opts)
			}, tc.want...)
		})
	}
}

// TestAReferenceToAModuleNobodyComposedIsRefused is V23's two halves on the fake:
// the target's shape refuses at mount (above), and its registration refuses where
// the whole resource list exists. No shipped module declares a reference yet, so
// this is the only place the question has both answers written down.
func TestAReferenceToAModuleNobodyComposedIsRefused(t *testing.T) {
	resources := []httpx.Resource{
		{Module: "task", Entity: "task", Schema: entity.Schema{Fields: []entity.Field{
			{Name: "assigneeId", Type: entity.TypeUUID,
				Presentation: entity.FieldHints{Reference: &entity.FieldReference{Resource: "site/setting"}}}}}},
		{Module: "user", Entity: "user"},
	}
	if bad := rest.CheckReferences(resources); bad == "" {
		t.Fatal("a reference to a resource that is not registered is served")
	}
	good := []httpx.Resource{
		{Module: "task", Entity: "task", Schema: entity.Schema{Fields: []entity.Field{
			{Name: "assigneeId", Type: entity.TypeUUID,
				Presentation: entity.FieldHints{Reference: &entity.FieldReference{Resource: "user/user"}}}}}},
		{Module: "user", Entity: "user"},
	}
	if bad := rest.CheckReferences(good); bad != "" {
		t.Errorf("CheckReferences refuses a wiring that composes what it names: %s", bad)
	}
}
