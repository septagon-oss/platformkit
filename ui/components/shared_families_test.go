package components_test

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	c "github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	g "maragu.dev/gomponents"
)

func TestSharedMoneyUsesExactMinorUnitsAndExplicitAdjustments(t *testing.T) {
	for _, tc := range []struct{ unit, quantity, want int64 }{{125, 2, 250}, {200, 1, 200}, {9007199254740993, 1, 9007199254740993}, {0, math.MaxInt64, 0}} {
		got, err := c.LineTotal(c.Money{Minor: tc.unit, Currency: "EUR"}, tc.quantity)
		if err != nil || got.Minor != tc.want {
			t.Fatalf("line total: %+v, %v", got, err)
		}
	}
	money := func(n int64, currency c.Currency) *c.MoneyText {
		return &c.MoneyText{Money: c.Money{Minor: n, Currency: currency}, Text: "amount", AccessibleText: "amount"}
	}
	lines := []c.SummaryLine{{Key: "discount", Label: "Discount", Effect: "add", Amount: money(-50, "EUR")}, {Key: "shipping", Label: "Shipping", Effect: "add", Amount: money(25, "EUR")}, {Key: "tax", Label: "Included tax", Effect: "included", Amount: money(75, "EUR")}}
	total, err := c.OrderTotal(c.Money{Minor: 450, Currency: "EUR"}, lines)
	if err != nil || total.Minor != 425 {
		t.Fatalf("total = %+v, %v", total, err)
	}
	if lines[0].Amount.Money.Minor != -50 {
		t.Fatal("mutated caller input")
	}
	if _, err := c.LineTotal(c.Money{Minor: math.MaxInt64, Currency: "EUR"}, 2); err == nil {
		t.Fatal("overflow accepted")
	}
	lines[0].Amount = money(-50, "USD")
	if _, err := c.OrderTotal(c.Money{Minor: 450, Currency: "EUR"}, lines); err == nil {
		t.Fatal("mixed currencies accepted")
	}
	lines[0].Amount = nil
	if _, err := c.OrderTotal(c.Money{Minor: 450, Currency: "EUR"}, lines); err == nil {
		t.Fatal("unknown adjustment became zero")
	}
	if _, err := c.OrderTotal(c.Money{Minor: math.MaxInt64, Currency: "EUR"}, []c.SummaryLine{{Key: "one", Label: "One", Effect: "add", Amount: money(1, "EUR")}}); err == nil {
		t.Fatal("addition overflow accepted")
	}
}
func TestSharedQuantityDoesNotClampOrLosePrecision(t *testing.T) {
	for _, tc := range []struct {
		v, min, max, step int64
		want              bool
	}{{2, 2, 9, 2, true}, {3, 2, 9, 2, false}, {8, 2, 9, 2, true}, {9, 2, 9, 2, false}, {10, 2, 9, 2, false}, {9007199254740993, 0, math.MaxInt64, 1, true}, {math.MaxInt64, 0, math.MaxInt64, 2, false}, {0, 0, 1, 0, false}, {-1, 0, 9, 1, false}} {
		if got := c.ValidQuantity(tc.v, tc.min, tc.max, tc.step); got != tc.want {
			t.Fatalf("quantity %+v = %v", tc, got)
		}
	}
}
func TestSharedSlotPrecedenceUsesExplicitUTCNow(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	base := c.Slot{ID: "one", StartUTC: now.Add(time.Hour), EndUTC: now.Add(2 * time.Hour), Availability: "available", Capacity: new(int64(3)), Remaining: new(int64(1))}
	for _, tc := range []struct {
		name   string
		change func(*c.Slot)
		qty    int64
		stale  bool
		want   string
	}{{"one seat", func(*c.Slot) {}, 1, false, "available"}, {"two seats", func(*c.Slot) {}, 2, false, "full"}, {"at start", func(s *c.Slot) { s.StartUTC = now }, 1, false, "past"}, {"booked wins", func(s *c.Slot) { s.Availability = "booked"; s.StartUTC = now }, 1, false, "booked"}, {"stale wins", func(s *c.Slot) { s.Availability = "booked" }, 1, true, "stale"}, {"unknown available", func(s *c.Slot) { s.Capacity = nil; s.Remaining = nil }, 1, false, "available"}, {"unknown unavailable", func(s *c.Slot) { s.Capacity = nil; s.Remaining = nil; s.Availability = "unavailable" }, 1, false, "unavailable"}} {
		t.Run(tc.name, func(t *testing.T) {
			slot := base
			tc.change(&slot)
			got, err := c.SlotEligibility(slot, now, tc.qty, tc.stale)
			if err != nil || got != tc.want {
				t.Fatalf("%s %v", got, err)
			}
		})
	}
	bad := base
	bad.Remaining = new(int64(4))
	if _, err := c.SlotEligibility(bad, now, 1, false); err == nil {
		t.Fatal("inconsistent capacity accepted")
	}
	bad = base
	bad.StartUTC = bad.StartUTC.In(time.FixedZone("caller", 3600))
	if _, err := c.SlotEligibility(bad, now, 1, false); err == nil {
		t.Fatal("non-UTC instant accepted")
	}
}
func TestSharedCalendarUsesCivilDaysAcrossDSTAndExclusiveEnds(t *testing.T) {
	for _, tc := range []struct {
		date, next string
		hours      int
	}{{"2026-03-29", "2026-03-30", 23}, {"2026-10-25", "2026-10-26", 25}} {
		t.Run(tc.date, func(t *testing.T) {
			zone, err := time.LoadLocation("Europe/Lisbon")
			if err != nil {
				t.Fatal(err)
			}
			start, _ := time.ParseInLocation(time.DateOnly, tc.date, zone)
			end, _ := time.ParseInLocation(time.DateOnly, tc.next, zone)
			if end.Sub(start) != time.Duration(tc.hours)*time.Hour {
				t.Fatal("fixture has wrong DST duration")
			}
			p := c.CalendarProps{ComponentProps: c.ComponentProps{ID: "calendar"}, Label: "Events", TimeZone: "Europe/Lisbon", TimeZoneLabel: "Lisbon", Language: "en", NowUTC: start.UTC(), RangeStartDate: tc.date, RangeEndDate: end.AddDate(0, 0, 1).Format(time.DateOnly), AllDayLabel: "All day", AgendaLabel: "Agenda", GridLabel: "Grid", FallbackText: "Agenda remains available", DateStrip: c.DateStripProps{Label: "Date", SelectedDate: tc.date, Days: []c.DateChoice{{Date: tc.date, Label: tc.date, Href: "/calendar"}}}, Events: []c.CalendarEvent{{ID: "timed", Title: "Timed", TimeText: "Local day", StatusLabel: "Available", StartUTC: start.UTC(), EndUTC: end.UTC()}, {ID: "all-day", Title: "All day", TimeText: "One day", StatusLabel: "Available", AllDay: true, StartDate: tc.date, EndDate: tc.next}}}
			days, err := c.CalendarAgenda(p)
			if err != nil {
				t.Fatal(err)
			}
			if len(days) != 2 || len(days[0].Events) != 2 || len(days[1].Events) != 0 || days[0].Events[0].ID != "all-day" {
				t.Fatalf("incorrect half-open intersection: %+v", days)
			}
		})
	}
}
func TestSharedChartGeometryAndDisconnectedGaps(t *testing.T) {
	points := []c.ChartPoint{{Key: "a", X: 0, Y: new(0.0), XText: "a", YText: "0"}, {Key: "b", X: 10, Y: new(10.0), XText: "b", YText: "10"}}
	b, found, err := c.ChartRange(points, false)
	if err != nil || !found {
		t.Fatal(err)
	}
	for _, tc := range []struct{ x, y, wantX, wantY float64 }{{0, 0, 0, 100}, {10, 10, 100, 0}, {5, 5, 50, 50}} {
		x, y, err := c.ChartPosition(b, tc.x, tc.y)
		if err != nil || x != tc.wantX || y != tc.wantY {
			t.Fatalf("coordinate %v,%v, %v", x, y, err)
		}
	}
	flat := []c.ChartPoint{{Key: "a", X: 0, Y: new(7.0), XText: "a", YText: "7"}}
	bounds, _, err := c.ChartRange(flat, false)
	if err != nil || bounds.MinY != 6 || bounds.MaxY != 8 {
		t.Fatalf("flat bounds %+v %v", bounds, err)
	}
	for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		points[0].Y = new(bad)
		if _, _, err := c.ChartRange(points, false); err == nil {
			t.Fatal("nonfinite chart accepted")
		}
	}
	gap := c.SparklineProps{Label: "Trend", Summary: "A missing middle sample", Points: []c.ChartPoint{{Key: "a", X: 0, Y: new(1.0), XText: "a", YText: "1"}, {Key: "b", X: 1, XText: "b", YText: "Unknown"}, {Key: "c", X: 2, Y: new(3.0), XText: "c", YText: "3"}}}
	var out bytes.Buffer
	if err := c.Sparkline(gap).Render(&out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "<path") || strings.Count(out.String(), "<circle") != 2 {
		t.Fatal("missing sample was connected")
	}
}
func TestSharedAbsentDataRefusesBeforeRendering(t *testing.T) {
	refused := c.ContentState{Status: c.MediaRefused, Title: "Access", Text: "No access"}
	for name, node := range map[string]g.Node{
		"stepper":  c.Stepper(c.StepperProps{Label: "Flow", State: refused, CurrentKey: "private"}),
		"calendar": c.Calendar(c.CalendarProps{Label: "Events", State: refused, Events: []c.CalendarEvent{{ID: "private"}}}),
		"map":      c.MapView(c.MapViewProps{Label: "Map", State: refused, Points: []c.MapPoint{{ID: "private"}}}),
		"photo":    c.PhotoGallery(c.PhotoGalleryProps{Label: "Photos", State: refused, Items: []c.Photo{{ID: "private"}}}),
		"product":  c.ProductCard(c.ProductCardProps{Label: "Product", State: refused, Media: c.MediaProps{Alt: "private"}}),
		"summary":  c.OrderSummary(c.OrderSummaryProps{Label: "Summary", State: refused, Total: &c.MoneyText{Text: "private"}}),
		"chart":    c.StatTile(c.StatTileProps{Label: "Value", State: refused, ValueText: "private"}),
	} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			if err := node.Render(&out); err == nil || out.Len() != 0 {
				t.Fatalf("refusal emitted %q, %v", out.String(), err)
			}
		})
	}
}
func TestSharedCartAndPlanDecisionsAreSuppliedNotInferred(t *testing.T) {
	var cart c.CartProps
	var plans c.PlanComparisonProps
	for _, example := range examples.Gallery() {
		if example.ID == "pk-ui.component.cart/default-en" {
			d, err := example.Describe()
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(d.Props, &cart); err != nil {
				t.Fatal(err)
			}
		}
		if example.ID == "pk-ui.component.plan-comparison/default-en" {
			d, err := example.Describe()
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(d.Props, &plans); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !cart.CanCheckout() {
		t.Fatal("valid quote cannot check out")
	}
	cart.QuoteState = "stale"
	if cart.CanCheckout() {
		t.Fatal("stale quote can check out")
	}
	cart.QuoteState = "current"
	cart.Lines[0].Availability = "sold-out"
	if cart.CanCheckout() {
		t.Fatal("sold-out quote can check out")
	}
	cart.Lines = nil
	if cart.CanCheckout() {
		t.Fatal("empty cart can check out")
	}
	plans.Plans[0].Features = nil
	if err := plans.Validate(); err == nil {
		t.Fatal("missing comparison cell accepted")
	}
}

func TestSharedMapCanRefuseDetailWhileRetainingAuthorizedPoints(t *testing.T) {
	var p c.MapViewProps
	for _, example := range examples.Gallery() {
		if example.ID == "pk-ui.component.map-view/default-en" {
			d, err := example.Describe()
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(d.Props, &p); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	p.ID = "map"
	p.SelectedID = "one"
	p.Detail = &c.DetailSheetProps{Open: true, DetailPanelProps: c.DetailPanelProps{Label: "Detail", CloseLabel: "Close", ReturnHref: "/places", State: c.ContentState{Status: c.MediaRefused, Title: "Access", Text: "No access to this detail"}}}
	calls := 0
	slots := c.MapViewSlots{SelectedContent: func(c.MapPoint) g.Node { calls++; return g.Text("private-map-detail") }}
	var out bytes.Buffer
	if err := c.MapViewWithSlots(p, slots).Render(&out); err != nil {
		t.Fatal(err)
	}
	if calls != 0 || strings.Contains(out.String(), "private-map-detail") || !strings.Contains(out.String(), "No access to this detail") || !strings.Contains(out.String(), "/places/one") {
		t.Fatalf("refused detail lost isolation: %d %s", calls, out.String())
	}
	p.Points[0].Longitude = math.NaN()
	out.Reset()
	if err := c.MapView(p).Render(&out); err == nil || out.Len() != 0 {
		t.Fatal("nonfinite location rendered")
	}
}
