package resource_test

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func TestResolvedValuesKeepMissingFalseAndExactAmountsDistinct(t *testing.T) {
	boolean := present.ResolvedField{Name: "v", Kind: "boolean", Reading: present.ResolvedReading{Missing: "absent", True: "Sim", False: "Não"}}
	integer := present.ResolvedField{Name: "v", Kind: "number", Reading: present.ResolvedReading{Missing: "absent", Precision: new(0)}}
	money := integer
	money.Reading.Money = &present.ResolvedMoney{CurrencyField: "currency", Scale: 2}
	cases := []struct {
		name           string
		field          present.ResolvedField
		value          any
		currency, want string
		refuse         bool
	}{
		{"missing bool", boolean, nil, "", "absent", false}, {"false", boolean, false, "", "Não", false}, {"true", boolean, true, "", "Sim", false},
		{"not boolean", boolean, "false", "", "", true}, {"zero", integer, int64(0), "", "0", false},
		{"large integer", integer, json.Number("9007199254740993"), "", "9007199254740993", false},
		{"fraction", integer, json.Number("1.5"), "", "", true}, {"overflow", integer, json.Number("9223372036854775808"), "", "", true},
		{"money", money, int64(-123), "EUR", "EUR -1.23", false}, {"zero money", money, int64(0), "USD", "USD 0.00", false},
		{"large money", money, json.Number("9007199254740993"), "EUR", "EUR 90071992547409.93", false},
		{"min money", money, int64(math.MinInt64), "EUR", "EUR -92233720368547758.08", false},
		{"unsafe float", money, float64(9007199254740992), "EUR", "", true}, {"missing currency", money, int64(123), "", "", true},
		{"invalid currency", money, int64(123), "eur", "", true}, {"missing amount", money, nil, "", "absent", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var b strings.Builder
			err := resource.Value(c.field, map[string]any{"v": c.value, "currency": c.currency}, opts).Render(&b)
			if c.refuse {
				if err == nil {
					t.Fatalf("accepted invalid value: %s", b.String())
				}
				return
			}
			if err != nil || b.String() != c.want {
				t.Fatalf("value=%q, %v; want %q", b.String(), err, c.want)
			}
		})
	}
	for scale, want := range []string{"EUR 123", "EUR 12.3", "EUR 1.23", "EUR 0.123"} {
		money.Reading.Money = &present.ResolvedMoney{CurrencyField: "currency", Scale: scale}
		var b strings.Builder
		err := resource.Value(money, map[string]any{"v": int64(123), "currency": "EUR"}, opts).Render(&b)
		if err != nil || b.String() != want {
			t.Fatalf("scale %d=%q, %v", scale, b.String(), err)
		}
	}
}

func TestResolvedTimesUseOneClockAndTheViewerZone(t *testing.T) {
	zone, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 3, 8, 7, 1, 0, 0, time.UTC)
	o := opts
	o.Location = zone
	o.Rendered = now
	r := note()
	r.Schema.Fields = []entity.Field{{Name: "title", Type: entity.TypeString}, {Name: "date", Type: entity.TypeTime, Presentation: entity.FieldHints{Format: "date"}}, {Name: "absolute", Type: entity.TypeTime}, {Name: "relative", Type: entity.TypeTime, Presentation: entity.FieldHints{Format: "relative"}}}
	row := map[string]any{"id": "1", "title": "Clock", "date": "2026-03-08T00:30:00Z", "absolute": "2026-03-08T06:59:59Z", "relative": "2026-03-08T07:02:59Z"}
	markup := render(t, resource.List(r, o, []map[string]any{row}, 1, 1, "", false).Body)
	for _, want := range []string{">2026-03-07</time>", ">2026-03-08 01:59</time>", ">in 1 minute</time>", `data-time-zone="America/New_York"`, `data-time-offset="-18000"`, `data-time-offset="-14400"`, `data-time-count="1"`, `data-time-unit="minute"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("missing %s", want)
		}
	}
	field := present.ResolvedField{Name: "v", Reading: present.ResolvedReading{Time: &present.ResolvedTime{Kind: "instant", Relative: true}}}
	for _, c := range []struct {
		distance          time.Duration
		text, unit, count string
	}{{-999 * time.Millisecond, "now", "second", "0"}, {-59 * time.Second, "59 seconds ago", "second", "-59"}, {-61 * time.Second, "1 minute ago", "minute", "-1"}, {-8 * 24 * time.Hour, "8 days ago", "day", "-8"}, {90 * time.Second, "in 1 minute", "minute", "1"}} {
		var b strings.Builder
		err := resource.Value(field, map[string]any{"v": now.Add(c.distance)}, o).Render(&b)
		if err != nil || !strings.Contains(b.String(), ">"+c.text+"</time>") || !strings.Contains(b.String(), `data-time-unit="`+c.unit+`"`) || !strings.Contains(b.String(), `data-time-count="`+c.count+`"`) {
			t.Errorf("relative: %s %v", b.String(), err)
		}
	}
	var b strings.Builder
	if err := resource.Value(field, map[string]any{"v": "not <a> time"}, o).Render(&b); err != nil || b.String() != "not &lt;a&gt; time" {
		t.Errorf("malformed time=%s %v", b.String(), err)
	}
}

func TestSupportedPolicyWinsWholeAndAnUnknownVersionFallsBackWhole(t *testing.T) {
	r := note()
	p := present.Resolve(present.Input{Schema: r.Schema, Immutable: r.Immutable})
	p.Identity.Plural = "resolved rows"
	p.Layout.Columns = []string{"pinned", "title"}
	p.List.Sorts = []present.ResolvedSortOption{}
	p.List.Filters = []present.ResolvedFilter{}
	p.List.DefaultSort = ""
	p.Titles.ListCandidates = []string{"body"}
	p.Titles.RecordCandidates = []string{"body"}
	p.Layout.Information = []string{}
	p.Layout.Overview = []string{}
	p.Layout.Sections = []present.ResolvedSection{}
	r.Resolved = &p
	r.Present = entity.EntryHints{Plural: "WRONG", PrimaryField: "title", Sortable: []string{"title"}}
	row := map[string]any{"id": "one", "title": "Wrong title", "body": "Resolved title", "pinned": false}
	html := render(t, resource.List(r, opts, []map[string]any{row}, 1, 1, "", false).Body)
	if !strings.Contains(html, "Resolved rows") || !strings.Contains(html, ">Resolved title<") || strings.Contains(html, "sort=") || strings.Contains(html, "WRONG") {
		t.Fatal(html)
	}
	if out := render(t, resource.Detail(r, opts, row, false).Body); strings.Contains(out, "<dt") {
		t.Fatal("empty membership gained raw fields")
	}
	p.Layout.Columns = []string{}
	if out := render(t, resource.List(r, opts, []map[string]any{row}, 1, 1, "", false).Body); strings.Contains(out, "<table") || strings.Contains(out, "No ") || !strings.Contains(out, "1 note") {
		t.Fatal("empty columns changed the read state")
	}
	p.PolicyVersion++
	if out := resource.List(r, opts, nil, 0, 1, "", false); out.Title != "WRONG" {
		t.Fatal("unsupported policy was mixed with old declarations")
	}
	p.PolicyVersion = present.PolicyVersion
	p.Layout.Columns = []string{"absent"}
	var b strings.Builder
	for _, node := range resource.List(r, opts, nil, 0, 1, "", false).Body {
		if err := node.Render(&b); err != nil {
			return
		}
	}
	t.Fatal("invalid supported policy did not refuse")
}

func TestSupportedPolicyRefusesMissingLabelsAndQueryReferences(t *testing.T) {
	for _, broken := range []struct {
		name string
		edit func(*present.Resolved)
	}{
		{"field label", func(p *present.Resolved) { p.Fields[0].Label = "" }},
		{"sort field", func(p *present.Resolved) {
			p.List.Sorts = []present.ResolvedSortOption{{Field: "absent", Label: "Missing", Query: "absent"}}
		}},
		{"filter field", func(p *present.Resolved) {
			p.List.Filters = []present.ResolvedFilter{{Field: "absent", Label: "Missing"}}
		}},
	} {
		t.Run(broken.name, func(t *testing.T) {
			r := note()
			p := present.Resolve(present.Input{Schema: r.Schema})
			broken.edit(&p)
			r.Resolved = &p
			var out strings.Builder
			for _, node := range resource.List(r, opts, nil, 0, 1, "", false).Body {
				if err := node.Render(&out); err != nil {
					return
				}
			}
			t.Fatal("invalid supported policy silently used a fallback")
		})
	}
}
