package entity_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
)

func deckHints() entity.EntryHints {
	return entity.EntryHints{Sortable: []string{"title", "rank", "hidden", "tags", "values"}, Deck: &entity.DeckHints{
		Queue: []string{"status:draft"}, Order: "title",
		Directions: map[string]entity.DeckDirection{
			"right": {Command: "publish", Label: "Publish", Tone: "success", Icon: "document"},
			"down":  {Skip: true},
		},
	}}
}

var deckFields = []entity.Field{
	{Name: "title", Type: entity.TypeString}, {Name: "status", Type: entity.TypeString, Enum: []string{"draft", "published"}},
	{Name: "rank", Type: entity.TypeInt}, {Name: "ratio", Type: entity.TypeFloat},
	{Name: "done", Type: entity.TypeBool}, {Name: "id", Type: entity.TypeUUID}, {Name: "at", Type: entity.TypeTime},
	{Name: "tags", Type: entity.TypeList}, {Name: "values", Type: entity.TypeMap},
	{Name: "hidden", Type: entity.TypeString, Presentation: entity.FieldHints{Visibility: "hidden"}},
}

func TestDeckRejectsInvalidQueueOrderAndDirections(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*entity.DeckHints)
		want   string
	}{
		{"empty queue", func(d *entity.DeckHints) { d.Queue = []string{} }, "deck.queue"},
		{"filter grammar", func(d *entity.DeckHints) { d.Queue = []string{"status in (draft)"} }, "status in (draft)"},
		{"unknown filter", func(d *entity.DeckHints) { d.Queue = []string{"missing:x"} }, "missing"},
		{"list filter", func(d *entity.DeckHints) { d.Queue = []string{"tags:x"} }, "tags"},
		{"map filter", func(d *entity.DeckHints) { d.Queue = []string{"values:x"} }, "values"},
		{"bad integer", func(d *entity.DeckHints) { d.Queue = []string{"rank:no"} }, "rank"},
		{"empty bool", func(d *entity.DeckHints) { d.Queue = []string{"done:"} }, "done"},
		{"bad uuid", func(d *entity.DeckHints) { d.Queue = []string{"id:no"} }, "id"},
		{"bad instant", func(d *entity.DeckHints) { d.Queue = []string{"at:yesterday"} }, "at"},
		{"empty order", func(d *entity.DeckHints) { d.Order = "" }, "deck.order"},
		{"minus only", func(d *entity.DeckHints) { d.Order = "-" }, "deck.order"},
		{"double minus", func(d *entity.DeckHints) { d.Order = "--title" }, "--title"},
		{"plus", func(d *entity.DeckHints) { d.Order = "+title" }, "+title"},
		{"not sortable", func(d *entity.DeckHints) { d.Order = "status" }, "status"},
		{"hidden order", func(d *entity.DeckHints) { d.Order = "hidden" }, "hidden"},
		{"list order", func(d *entity.DeckHints) { d.Order = "tags" }, "tags"},
		{"map order", func(d *entity.DeckHints) { d.Order = "values" }, "values"},
		{"no directions", func(d *entity.DeckHints) { d.Directions = nil }, "deck.directions"},
		{"unknown directions sorted", func(d *entity.DeckHints) {
			d.Directions["z"] = entity.DeckDirection{}
			d.Directions["diagonal"] = entity.DeckDirection{}
		}, "directions.diagonal"},
		{"wrong skip", func(d *entity.DeckHints) { d.Directions["up"] = entity.DeckDirection{Skip: true} }, "directions.up.skip"},
		{"mixed skip", func(d *entity.DeckHints) { d.Directions["down"] = entity.DeckDirection{Skip: true, Label: "Later"} }, "directions.down.skip"},
		{"missing command", func(d *entity.DeckHints) { v := d.Directions["right"]; v.Command = ""; d.Directions["right"] = v }, "right.command"},
		{"blank label", func(d *entity.DeckHints) { v := d.Directions["right"]; v.Label = " \t"; d.Directions["right"] = v }, "right.label"},
		{"bad tone", func(d *entity.DeckHints) { v := d.Directions["right"]; v.Tone = "brand"; d.Directions["right"] = v }, "right.tone"},
		{"bad icon", func(d *entity.DeckHints) { v := d.Directions["right"]; v.Icon = "heart"; d.Directions["right"] = v }, "right.icon"},
		{"empty icon", func(d *entity.DeckHints) { v := d.Directions["right"]; v.Icon = ""; d.Directions["right"] = v }, "right.icon"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := deckHints()
			tc.change(p.Deck)
			if bad := entity.DeckFault(p, deckFields); !strings.Contains(bad, tc.want) {
				t.Fatalf("fault = %q, want %q", bad, tc.want)
			}
		})
	}
}

func TestDeckUsesLiteralScalarListFilters(t *testing.T) {
	for _, queue := range [][]string{nil, {"title:"}, {"title:a:b,c"}, {"rank:1", "rank:2"}, {"done:false", "rank:0", "ratio:1.5"}, {"status:outside-enum"}, {"hidden:yes"}, {"at:2026-01-02T03:04:05Z"}} {
		p := deckHints()
		p.Deck.Queue = queue
		p.Deck.Order = "-title"
		if bad := entity.DeckFault(p, deckFields); bad != "" {
			t.Fatal(bad)
		}
	}
	p := deckHints()
	p.Deck.Directions = map[string]entity.DeckDirection{"down": {Skip: true}}
	if bad := entity.DeckFault(p, deckFields); bad != "" {
		t.Fatal(bad)
	}
}
