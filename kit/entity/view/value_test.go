package view_test

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// domainSchema is the task schema widened with the fields only the value domain
// needs: two plain numbers, two amounts at different scales, and an instant read
// as a calendar day.
func domainInput(row map[string]any, zone *time.Location) view.Input {
	s := taskSchema()
	s.Fields = append(s.Fields,
		entity.Field{Name: "count", Type: entity.TypeInt},
		entity.Field{Name: "rate", Type: entity.TypeFloat},
		entity.Field{Name: "fee", Type: entity.TypeInt, Presentation: entity.FieldHints{
			Money: &entity.FieldMoney{CurrencyField: "currency", Scale: 0}}},
		entity.Field{Name: "dayAt", Type: entity.TypeTime, Presentation: entity.FieldHints{Format: "date"}},
	)
	in := input(view.KindRecord, []map[string]any{row}, nil)
	in.Resource.Schema = s
	in.Policy = taskPolicy(s, []present.CommandInput{commandInput("archive"), commandInput("assign")}, nil, false)
	in.State.Zone = zone
	return in
}

func recordCells(t *testing.T, in view.Input) map[string]view.Field {
	t.Helper()
	page := build(t, in)
	out := map[string]view.Field{}
	for _, s := range page.Sections {
		for _, f := range s.Fields {
			out[f.Name] = f
		}
	}
	if len(out) == 0 {
		t.Fatal("the record shows no fields")
	}
	return out
}

func TestALargeIntegerKeepsItsDigitsInBothSigns(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		stored any
		kind   view.ValueKind
		value  any
	}{
		{json.Number("9007199254740991"), view.ValueInteger, int64(9007199254740991)},
		{json.Number("9007199254740992"), view.ValueBigInteger, "9007199254740992"},
		{json.Number("-9007199254740993"), view.ValueBigInteger, "-9007199254740993"},
		{json.Number("-9007199254740991"), view.ValueInteger, int64(-9007199254740991)},
		{json.Number("0"), view.ValueInteger, int64(0)},
		{int64(-9007199254740993), view.ValueBigInteger, "-9007199254740993"},
	} {
		fields := recordCells(t, domainInput(map[string]any{"id": "t1", "title": "n", "count": tc.stored}, time.UTC))
		got, ok := fields["count"]
		if !ok {
			t.Fatal("the count field is not on the page")
		}
		if got.ValueKind != tc.kind || got.Value != tc.value {
			t.Errorf("count %v is %s %v, want %s %v", tc.stored, got.ValueKind, got.Value, tc.kind, tc.value)
		}
		if err := json.Unmarshal([]byte(`{"v":`+string(bytes(t, got.Value))+`}`), &struct{}{}); err != nil {
			t.Errorf("count %v does not spell JSON: %v", tc.stored, err)
		}
	}
}

func TestAValueDecodedWithoutUseNumberIsWrittenAsADecimal(t *testing.T) {
	t.Parallel()
	fields := recordCells(t, domainInput(map[string]any{"id": "t1", "title": "n", "count": float64(9007199254740993)}, time.UTC))
	got := fields["count"]
	if got.ValueKind != view.ValueDecimal {
		t.Fatalf("a count that reached us through a double is %s: the shape must stay one a double cannot round", got.ValueKind)
	}
	if text, isString := got.Value.(string); !isString || text == "" {
		t.Fatalf("value %v, want the digits that survived that decode as a string", got.Value)
	}
}

func TestAnAmountIsMinorUnitsWithItsCurrency(t *testing.T) {
	t.Parallel()
	fields := recordCells(t, domainInput(map[string]any{
		"id": "t1", "title": "n", "amount": json.Number("-9007199254740993"), "currency": "EUR", "fee": json.Number("400"),
	}, time.UTC))
	amount := fields["amount"].Value
	money, ok := amount.(view.Money)
	if !ok {
		t.Fatalf("amount %T, want Money", amount)
	}
	if money.MinorUnits != "-9007199254740993" || money.Currency != "EUR" || money.Scale != 2 {
		t.Errorf("amount %+v", money)
	}
	if text := fields["amount"].Text; text != "-9007199254740993" {
		t.Errorf("the plain reading of the amount is %q, which the screens already printed", text)
	}
	fee := fields["fee"].Value.(view.Money)
	if fee.MinorUnits != "400" || fee.Scale != 0 {
		t.Errorf("fee %+v, want 400 units at scale 0", fee)
	}
}

func TestAnAmountWithoutItsCurrencySiblingSaysSo(t *testing.T) {
	t.Parallel()
	fields := recordCells(t, domainInput(map[string]any{"id": "t1", "title": "n", "amount": json.Number("1250")}, time.UTC))
	money := fields["amount"].Value.(view.Money)
	if money.Currency != "" || money.MinorUnits != "1250" {
		t.Errorf("amount %+v: the currency of a value is a fact about the record, not about the declaration", money)
	}
}

func TestAnAbsentValueIsNotAFalseOne(t *testing.T) {
	t.Parallel()
	fields := recordCells(t, domainInput(map[string]any{
		"id": "t1", "title": "n", "done": false, "labels": []any{}, "meta": map[string]any{},
	}, time.UTC))
	for name, want := range map[string]view.ValueKind{
		"dueAt": view.ValueAbsent, "done": view.ValueBoolean, "labels": view.ValueList, "meta": view.ValueMap,
	} {
		if got := fields[name].ValueKind; got != want {
			t.Errorf("%s is %s, want %s", name, got, want)
		}
	}
	if fields["dueAt"].Value != nil || fields["dueAt"].Text != "—" {
		t.Errorf("an absent field is %q/%v, want the resolved missing word and a null value", fields["dueAt"].Text, fields["dueAt"].Value)
	}
	if fields["done"].Value != false || fields["done"].Text != "No" {
		t.Errorf("false is %q/%v: a boolean is never the dash that means nothing was stored", fields["done"].Text, fields["done"].Value)
	}
	if raw := string(bytes(t, fields["labels"].Value)); raw != "[]" {
		t.Errorf("an empty list is %s", raw)
	}
	if raw := string(bytes(t, fields["meta"].Value)); raw != "{}" {
		t.Errorf("an empty map is %s", raw)
	}
}

func TestAnInstantAndACalendarDayAreTheSameInstant(t *testing.T) {
	t.Parallel()
	zone, err := time.LoadLocation("Europe/Lisbon")
	if err != nil {
		t.Skipf("no zone database to load a zone from: %v", err)
	}
	row := map[string]any{"id": "t1", "title": "n", "dueAt": "2026-07-01T23:30:00Z", "dayAt": "2026-07-01T23:30:00Z"}
	at := recordCells(t, domainInput(row, zone))
	if got, want := at["dueAt"].Value, "2026-07-01T23:30:00.000000Z"; at["dueAt"].ValueKind != view.ValueInstant || got != want {
		t.Errorf("instant %v (%s), want %s", got, at["dueAt"].ValueKind, want)
	}
	if got, want := at["dayAt"].Value, "2026-07-02"; at["dayAt"].ValueKind != view.ValueDate || got != want {
		t.Errorf("date %v (%s), want %s", got, at["dayAt"].ValueKind, want)
	}
	if at["dueAt"].Text != "2026-07-01 23:30" {
		t.Errorf("the words of the instant are %q, which the screens print today", at["dueAt"].Text)
	}
	page := build(t, domainInput(row, zone))
	if page.Zone != "Europe/Lisbon" {
		t.Errorf("zone %q: a page says which zone its days were cut in", page.Zone)
	}
	utc := recordCells(t, domainInput(row, nil))
	if utc["dayAt"].Value != "2026-07-01" || utc["dueAt"].Value != "2026-07-01T23:30:00.000000Z" {
		t.Errorf("with no zone loaded the day is UTC's: %+v", utc["dayAt"])
	}
}

func TestAnUndeclaredEnumValueKeepsItsOwnTextAndANeutralTone(t *testing.T) {
	t.Parallel()
	fields := recordCells(t, domainInput(map[string]any{"id": "t1", "title": "n", "status": "in_progress"}, time.UTC))
	status := fields["status"]
	if status.Text != "in_progress" {
		t.Errorf("text %q: a value nobody declared is not humanised into words nobody wrote", status.Text)
	}
	if status.Tone != "neutral" {
		t.Errorf("tone %q, want neutral", status.Tone)
	}
	declared := recordCells(t, domainInput(map[string]any{"id": "t1", "title": "n", "status": "done"}, time.UTC))["status"]
	if declared.Text != "Done" || declared.Tone != "success" {
		t.Errorf("the declared value lost its words or its tone: %+v", declared)
	}
}

func TestANonFiniteNumberKeepsItsTextAndNullsItsValue(t *testing.T) {
	t.Parallel()
	for _, f := range []float64{math.Inf(1), math.Inf(-1), math.NaN()} {
		rate := recordCells(t, domainInput(map[string]any{"id": "t1", "title": "n", "rate": f}, time.UTC))["rate"]
		if rate.ValueKind != view.ValueRaw || rate.Value != nil {
			t.Errorf("%v is %s/%v, want raw and a null value: JSON has no spelling for it", f, rate.ValueKind, rate.Value)
		}
		if rate.Text == "" || rate.Text == "—" {
			t.Errorf("the stored text of %v is %q, and a read is never lost", f, rate.Text)
		}
	}
}

func TestAReferenceFieldNamesItsTarget(t *testing.T) {
	t.Parallel()
	s := taskSchema()
	s.Fields = append(s.Fields, entity.Field{Name: "projectId", Type: entity.TypeUUID,
		Presentation: entity.FieldHints{Reference: &entity.FieldReference{Resource: "m/project"}}})
	in := input(view.KindRecord, []map[string]any{{"id": "t1", "title": "n", "projectId": "p1"}}, nil)
	in.Resource.Schema = s
	in.Policy = taskPolicy(s, nil, nil, false)
	field := recordCells(t, in)["projectId"]
	if field.Reference != "m/project" || field.ValueKind != view.ValueString || field.Value != "p1" {
		t.Errorf("reference field %+v", field)
	}
}

func TestARowWithoutAnIdIsKeptRatherThanInvented(t *testing.T) {
	t.Parallel()
	page := build(t, input(view.KindList, []map[string]any{{"title": "a row"}}, nil))
	if len(page.Table.Rows) != 1 || page.Table.Rows[0].ID != "" {
		t.Fatalf("rows %+v, want the row kept with no id of its own", page.Table.Rows)
	}
	if page.Table.Rows[0].Title != "a row" {
		t.Errorf("row title %q", page.Table.Rows[0].Title)
	}
}
