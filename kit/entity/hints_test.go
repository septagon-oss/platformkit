package entity_test

// hints_test.go pins the two promises the field level makes to its callers: one
// declaration per declaration shape, and one copy of every hint per call.

import (
	"reflect"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
)

type hinted struct {
	entity.Base
	Level  string `json:"level" enum:"low,high" enumLabels:"low=Low,high=High" enumTones:"low=neutral,high=warning"`
	Amount int64  `json:"amount" ui:"format:money;currency:unit;scale:2"`
	Unit   string `json:"unit"`
	Owner  string `json:"owner" ui:"reference:user/user;section:record"`
}

func (hinted) TableName() string { return "hints" }

func TestFieldsReturnsOneCopyOfEveryHint(t *testing.T) {
	first := entity.Fields[*hinted]()
	second := entity.Fields[*hinted]()
	for i := range first {
		if first[i].Name != second[i].Name {
			t.Fatalf("the second call answers %v", second)
		}
	}
	byName := func(fields []entity.Field) map[string]entity.Field {
		out := map[string]entity.Field{}
		for _, f := range fields {
			out[f.Name] = f
		}
		return out
	}
	a, b := byName(first), byName(second)
	a["level"].Presentation.EnumLabels["low"] = "Rewritten"
	a["level"].Presentation.EnumTones["high"] = "danger"
	a["amount"].Presentation.Money.Scale = 0
	a["owner"].Presentation.Reference.Resource = "x/x"
	if got := b["level"].Presentation.EnumLabels["low"]; got != "Low" {
		t.Errorf("one caller's label reached another caller's schema: %q", got)
	}
	if got := b["level"].Presentation.EnumTones["high"]; got != "warning" {
		t.Errorf("one caller's tone reached another caller's schema: %q", got)
	}
	if got := b["amount"].Presentation.Money.Scale; got != 2 {
		t.Errorf("one caller's money scale reached another caller's schema: %d", got)
	}
	if got := b["owner"].Presentation.Reference.Resource; got != "user/user" {
		t.Errorf("one caller's reference reached another caller's schema: %q", got)
	}
}

func TestTheVocabulariesRefuseWhatTheyDoNotList(t *testing.T) {
	if entity.ValidIcon("user") {
		t.Error("ValidIcon answers a ui/icon glyph name; the vocabulary is the semantic one")
	}
	if entity.ValidTone("") {
		t.Error("ValidTone must refuse the empty string: an absent tone is the key being absent")
	}
	if !entity.ValidVisibility("") || !entity.ValidFormat("") || !entity.ValidIcon("") {
		t.Error("the empty string means \"nobody declared this\" on the other three axes")
	}
	for _, list := range [][]string{entity.Icons, entity.Tones, entity.Formats, entity.Visibilities} {
		for _, name := range list {
			if !entity.ValidIcon(name) && !entity.ValidTone(name) && !entity.ValidFormat(name) && !entity.ValidVisibility(name) {
				t.Errorf("%q is listed and no predicate admits it", name)
			}
		}
	}
	if entity.ValidTone("brand") {
		t.Error("`brand` is a component affordance in ui/components and deliberately not a status a value may wear")
	}
}

func TestVisibilityIsAReadingDecision(t *testing.T) {
	hidden := entity.FieldHints{Visibility: "hidden"}
	if !hidden.Hidden() || !hidden.OffList() {
		t.Error("hidden must be off both the list and the record")
	}
	detail := entity.FieldHints{Visibility: "detail"}
	if detail.Hidden() || !detail.OffList() {
		t.Error("detail is off the list and on the record")
	}
	for _, h := range []entity.FieldHints{{}, {Visibility: "shown"}} {
		if h.Hidden() || h.OffList() {
			t.Errorf("visibility %+v keeps a field off a screen it did not ask to be kept off", h)
		}
	}
	if reflect.TypeOf(entity.Field{}.Presentation).Kind() != reflect.Struct {
		t.Error("Field.Presentation must be a value: the zero FieldHints is \"nobody said anything\"")
	}
}

// TestExplicitVisibilityDecidesTheListBesideHideList is the pair of ways to name
// one column resolved by precedence rather than by refusal, in both directions: the
// explicit hint wins over the older `hide:list` tag whether that puts the field back
// on the list or takes it off. A pair that agreed (`hidden` beside `hide:list`) is
// refused by neither, and silence is the older tag on its own.
func TestExplicitVisibilityDecidesTheListBesideHideList(t *testing.T) {
	for _, pair := range []struct {
		visibility string
		hideList   bool
		onList     bool
	}{
		{"", false, true}, {"", true, false},
		{"shown", true, true}, {"shown", false, true},
		{"detail", false, false}, {"detail", true, false},
		{"hidden", false, false}, {"hidden", true, false},
	} {
		f := entity.Field{HideList: pair.hideList,
			Presentation: entity.FieldHints{Visibility: pair.visibility}}
		if got := f.OnList(); got != pair.onList {
			t.Errorf("visibility %q beside hide:list=%v answers OnList %v, want %v",
				pair.visibility, pair.hideList, got, pair.onList)
		}
	}
}
