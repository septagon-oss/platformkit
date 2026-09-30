package examples

import (
	"fmt"
	"math"
	"time"

	c "github.com/septagon-oss/platformkit/ui/components"
	g "maragu.dev/gomponents"
)

type sharedCopy struct {
	locale, label, details, empty, loading, failed, refused, offline, retry, previous, next, close, unknown, available, unavailable, past, full, booked, stale, quantity, save, back, continueText, saveExit, status, summary, total, subtotal, price, period, included, excluded, plan, recommended, current, showData, timeZone, caption string
}

func sharedLocales() []sharedCopy {
	return []sharedCopy{
		{"en", "Items", "Review the details before continuing.", "There are no items to show.", "Loading items", "The information could not be loaded.", "You do not have access to this information.", "Offline: showing the last confirmed snapshot.", "Try again", "Previous", "Next", "Close", "Not yet known", "Available", "Unavailable", "In the past", "Full", "Booked", "Refresh before continuing.", "Quantity", "Save", "Back", "Continue", "Save and exit", "Status", "Summary", "Total", "Subtotal", "Price", "Billed each month", "Included", "Not included", "Plan", "Recommended", "Current plan", "Show the data", "Europe/Lisbon — times include UTC offsets", "A geometric study"},
		{"pt-PT", "Itens", "Reveja os detalhes antes de continuar.", "Não há itens para mostrar.", "A carregar itens", "Não foi possível carregar a informação.", "Não tem acesso a esta informação.", "Sem ligação: a mostrar a última informação confirmada.", "Tentar novamente", "Anterior", "Seguinte", "Fechar", "Ainda não se sabe", "Disponível", "Indisponível", "No passado", "Lotado", "Reservado", "Atualize antes de continuar.", "Quantidade", "Guardar", "Voltar", "Continuar", "Guardar e sair", "Estado", "Resumo", "Total", "Subtotal", "Preço", "Faturado mensalmente", "Incluído", "Não incluído", "Plano", "Recomendado", "Plano atual", "Mostrar os dados", "Europe/Lisbon — horas com desvio UTC", "Um estudo geométrico"},
	}
}
func sharedInfo(kind, story string, w sharedCopy) ExampleInfo {
	return ExampleInfo{ID: "pk-ui.component." + kind + "/" + story + "-" + w.locale, ComponentID: "pk-ui.component." + kind, Group: "Shared workflows", Name: kind + " / " + story + " / " + w.locale}
}
func sharedProps(kind string, w sharedCopy) c.ComponentProps {
	return c.ComponentProps{ID: kind + "-" + w.locale, Attrs: map[string]string{"lang": w.locale}}
}
func sharedState(story string, w sharedCopy) c.ContentState {
	state := c.ContentState{}
	switch story {
	case "loading":
		state = c.ContentState{Status: c.MediaLoading, LoadingLabel: w.loading}
	case "empty":
		state = c.ContentState{Status: c.MediaEmpty, Title: w.label, Text: w.empty}
	case "failed", "offline-failed":
		state = c.ContentState{Status: c.MediaFailed, Title: w.label, Text: w.failed}
	case "refused":
		state = c.ContentState{Status: c.MediaRefused, Title: w.label, Text: w.refused}
	}
	if story == "offline" || story == "offline-failed" {
		state.Offline = true
		state.OfflineText = w.offline
	}
	return state
}
func absent(state c.ContentState) bool { return state.Status != "" && state.Status != c.MediaReady }
func sharedMoney(minor int64, w sharedCopy) c.MoneyText {
	text := fmt.Sprintf("EUR %d.%02d", minor/100, minor%100)
	if minor < 0 {
		text = fmt.Sprintf("−EUR %d.%02d", -minor/100, -minor%100)
	}
	if w.locale == "pt-PT" {
		text = fmt.Sprintf("%d,%02d EUR", minor/100, minor%100)
		if minor < 0 {
			text = fmt.Sprintf("−%d,%02d EUR", -minor/100, -minor%100)
		}
	}
	return c.MoneyText{Money: c.Money{Minor: minor, Currency: "EUR"}, Text: text, AccessibleText: text}
}
func sharedDates(w sharedCopy) c.DateStripProps {
	today := "Today"
	if w.locale == "pt-PT" {
		today = "Hoje"
	}
	return c.DateStripProps{Label: w.label, TodayText: today, SelectedDate: "2026-10-25", Days: []c.DateChoice{
		{Date: "2026-10-24", Label: "24", Href: "/items?date=2026-10-24"}, {Date: "2026-10-25", Label: "25", Href: "/items?date=2026-10-25", Today: true}, {Date: "2026-10-26", Label: "26", Href: "/items?date=2026-10-26"},
	}}
}
func sharedQuantity(w sharedCopy) c.QuantityInputProps {
	return c.QuantityInputProps{ComponentProps: sharedProps("quantity", w), Label: w.quantity, Name: "quantity", Value: 2, Min: 2, Max: 9, Step: 2, DecreaseLabel: w.previous, IncreaseLabel: w.next, ValidationText: w.failed}
}
func sharedSummary(w sharedCopy) c.OrderSummaryProps {
	return c.OrderSummaryProps{Label: w.summary, Subtotal: new(sharedMoney(450, w)), Total: new(sharedMoney(425, w)), Complete: true, TotalLabel: w.total, SubtotalLabel: w.subtotal, Lines: []c.SummaryLine{
		{Key: "discount", Label: w.price, Effect: "add", Amount: new(sharedMoney(-50, w))}, {Key: "delivery", Label: w.details, Effect: "add", Amount: new(sharedMoney(25, w))}, {Key: "included", Label: w.included, Effect: "included", Amount: new(sharedMoney(75, w))},
	}}
}
func sharedPhoto(id string, w sharedCopy) c.Photo {
	media := c.MediaProps{Src: mediaSpecimen, Alt: w.caption, Width: 160, Height: 90, Caption: w.caption}
	return c.Photo{ID: id, Media: media, Full: media, Href: "/images/" + id, PositionText: w.caption + " · " + id}
}

func workflowExamples() []Example {
	var entries []Example
	for _, w := range sharedLocales() {
		for _, story := range []string{"default", "loading", "failed", "refused", "offline", "offline-failed", "first", "middle", "last", "step-error", "disabled-step", "save-pending", "save-failed", "save-succeeded", "resumed", "read-only"} {
			p := c.StepperProps{ComponentProps: sharedProps("stepper", w), Label: w.label, State: sharedState(story, w), ProgressText: "1 / 3", CurrentKey: "review", Steps: []c.Step{{Key: "choose", Title: w.label, State: "complete", Href: "/flow/choose"}, {Key: "review", Title: w.details}, {Key: "finish", Title: w.save, State: "upcoming"}}, FormID: "flow-" + w.locale}
			slots := c.StepperSlots{}
			if absent(p.State) {
				p.Steps = nil
				p.CurrentKey = ""
			} else {
				if story == "first" {
					p.CurrentKey = "choose"
					p.Steps[0].State = "upcoming"
					p.ProgressText = "0 / 3"
				}
				if story == "last" || story == "save-succeeded" {
					p.CurrentKey = "finish"
					p.Steps[1].State = "complete"
					p.ProgressText = "2 / 3"
				}
				if story == "step-error" {
					p.Steps[1].State = "error"
					slots.ErrorSummary = []g.Node{c.Notice(c.NoticeProps{Text: w.failed})}
				}
				if story == "disabled-step" {
					p.Steps[2].State = "disabled"
				}
				if story == "save-pending" {
					p.Busy = true
					p.SaveStatusText = w.loading
				}
				if story == "save-failed" {
					p.SaveStatusText = w.failed
				}
				if story == "save-succeeded" {
					p.SaveStatusText = w.save
				}
				p.Disabled = story == "read-only"
				slots.Body = []g.Node{c.Form(c.FormProps{ComponentProps: c.ComponentProps{ID: p.FormID}, Action: "/flow"}, c.Input(c.InputProps{Name: "notes", Label: w.details, Value: w.details, ComponentProps: c.ComponentProps{Attrs: map[string]string{"form": p.FormID}}}))}
				action := func(label, intent string) []g.Node {
					variant := "outline"
					if intent == "continue" {
						variant = "primary"
					}
					return []g.Node{c.Button(c.ButtonProps{ComponentProps: c.ComponentProps{Attrs: map[string]string{"form": p.FormID, "name": "intent", "value": intent}, Disabled: p.Disabled || p.Busy}, Label: label, Variant: variant, Type: "submit", Size: "lg"})}
				}
				slots.Back = action(w.back, "back")
				slots.Continue = action(w.continueText, "continue")
				slots.SaveExit = action(w.saveExit, "save-exit")
			}
			entries = append(entries, ExampleWithSlots(sharedInfo("stepper", story, w), p, slots, c.StepperWithSlots))
		}
		for _, story := range []string{"default", "today", "selected", "disabled-date", "long-label", "month-boundary", "keyboard-scroll"} {
			p := sharedDates(w)
			p.ComponentProps = sharedProps("date-strip", w)
			if story == "disabled-date" {
				p.Days[0].Disabled = true
				p.Days[0].Reason = w.unavailable
			}
			if story == "long-label" {
				p.Days[1].Label = w.details
			}
			if story == "month-boundary" {
				p.SelectedDate = "2026-11-01"
				p.Days = []c.DateChoice{{Date: "2026-10-31", Label: "31", Href: "/items?date=2026-10-31"}, {Date: "2026-11-01", Label: "1", Href: "/items?date=2026-11-01"}}
			}
			entries = append(entries, ExampleOf(sharedInfo("date-strip", story, w), p, c.DateStrip))
		}
		for _, story := range []string{"default", "loading", "empty", "failed", "refused", "offline", "offline-failed", "selected", "booked", "full", "capacity-unknown", "unavailable", "past", "overnight", "dst-fold", "stale", "invalid-selection", "confirmation-pending"} {
			p := c.SlotPickerProps{ComponentProps: sharedProps("slots", w), Label: w.label, State: sharedState(story, w), DateStrip: sharedDates(w), TimeZone: "Europe/Lisbon", TimeZoneLabel: w.timeZone, NowUTC: time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC), Name: "slot", Quantity: 1, SnapshotText: w.details, StatusLabels: c.SlotStatusLabels{Available: w.available, Booked: w.booked, Past: w.past, Unavailable: w.unavailable, Full: w.full, Stale: w.stale, CapacityUnknown: w.unknown}}
			p.Slots = []c.Slot{{ID: "early", StartUTC: time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC), EndUTC: time.Date(2026, 10, 25, 1, 0, 0, 0, time.UTC), TimeText: "01:30 UTC+01:00", Availability: "available", Capacity: new(int64(3)), Remaining: new(int64(1)), CapacityText: "1 / 3"}, {ID: "late", StartUTC: time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC), EndUTC: time.Date(2026, 10, 25, 2, 0, 0, 0, time.UTC), TimeText: "01:30 UTC+00:00", Availability: "available"}}
			if absent(p.State) {
				p.Slots = nil
			} else {
				switch story {
				case "selected":
					p.SelectedID = "early"
				case "booked":
					p.Slots[0].Availability = "booked"
				case "full":
					p.Quantity = 2
				case "capacity-unknown":
					p.Slots[0].Capacity = nil
					p.Slots[0].Remaining = nil
				case "unavailable":
					p.Slots[0].Availability = "unavailable"
				case "past":
					p.NowUTC = p.Slots[0].StartUTC
				case "overnight":
					p.Slots[0].StartUTC = time.Date(2026, 10, 25, 23, 30, 0, 0, time.UTC)
					p.Slots[0].EndUTC = time.Date(2026, 10, 26, 0, 30, 0, 0, time.UTC)
					p.Slots[0].TimeText = "23:30–00:30 UTC+00:00"
				case "stale":
					p.Stale = true
				case "invalid-selection":
					p.SelectedID = "removed"
					p.ErrorText = w.stale
				case "confirmation-pending":
					p.Disabled = true
				}
			}
			entries = append(entries, ExampleOf(sharedInfo("slot-picker", story, w), p, c.SlotPicker))
		}
	}
	return entries
}

func commerceExamples() []Example {
	var entries []Example
	for _, w := range sharedLocales() {
		for _, story := range []string{"default", "loading", "empty", "failed", "refused", "offline", "offline-failed", "no-image", "unknown-price", "sold-out", "unavailable", "promotion"} {
			p := c.ProductCardProps{ComponentProps: sharedProps("product", w), Label: w.label, State: sharedState(story, w)}
			if !absent(p.State) {
				p.ProductID = "sample"
				p.Title = w.caption
				p.Description = w.details
				p.Href = "/products/sample"
				p.Media = sharedPhoto("sample", w).Media
				p.Price = new(sharedMoney(125, w))
				p.Availability = "available"
				p.AvailabilityText = w.available
				switch story {
				case "no-image":
					p.Media = c.MediaProps{Status: c.MediaEmpty, Reason: w.empty}
				case "unknown-price":
					p.Price = nil
					p.PriceText = w.unknown
				case "sold-out", "unavailable":
					p.Availability = story
					p.AvailabilityText = w.unavailable
				case "promotion":
					p.Badges = []c.BadgeProps{{Label: w.recommended, Tone: "info"}}
				}
			}
			entries = append(entries, ExampleOf(sharedInfo("product-card", story, w), p, c.ProductCard))
		}
		for _, story := range []string{"default", "selected", "disabled-option", "sold-out", "required-error", "stale-choice", "long-label"} {
			p := c.OptionChipsProps{ComponentProps: sharedProps("options", w), Label: w.label, Name: "variant", Options: []c.Option{{Key: "one", Label: "A"}, {Key: "two", Label: "B"}}, Required: true}
			switch story {
			case "selected":
				p.Value = "one"
			case "disabled-option", "sold-out":
				p.Options[1].Disabled = true
				p.Options[1].Reason = w.unavailable
			case "required-error":
				p.ErrorText = w.failed
			case "stale-choice":
				p.Value = "removed"
				p.ErrorText = w.stale
			case "long-label":
				p.Options[0].Label = w.details
			}
			entries = append(entries, ExampleOf(sharedInfo("option-chips", story, w), p, c.OptionChips))
		}
		for _, story := range []string{"default", "minimum", "maximum", "invalid", "overflow-boundary", "disabled", "pending"} {
			p := sharedQuantity(w)
			switch story {
			case "maximum":
				p.Value = 8
			case "invalid":
				p.Value = 3
				p.ErrorText = w.failed
			case "overflow-boundary":
				p.Min = 0
				p.Max = math.MaxInt64
				p.Value = 9007199254740993
				p.Step = 1
			case "disabled":
				p.Disabled = true
			case "pending":
				p.Pending = true
			}
			entries = append(entries, ExampleOf(sharedInfo("quantity-input", story, w), p, c.QuantityInput))
		}
		for _, story := range []string{"default", "loading", "empty", "failed", "refused", "offline", "offline-failed", "sticky", "wrapped", "pending", "unavailable", "stale", "keyboard-open"} {
			p := c.BuyBarProps{ComponentProps: sharedProps("buy", w), Label: w.summary, State: sharedState(story, w)}
			slots := c.BuyBarSlots{}
			if !absent(p.State) {
				p.Total = new(sharedMoney(425, w))
				p.SummaryText = w.details
				p.Pending = story == "pending"
				if story == "unavailable" || story == "stale" {
					p.UnavailableText = w.stale
				}
				slots.PrimaryAction = []g.Node{c.Button(c.ButtonProps{Label: w.continueText, Href: "/checkout", Size: "lg"})}
			}
			entries = append(entries, ExampleWithSlots(sharedInfo("buy-bar", story, w), p, slots, c.BuyBarWithSlots))
		}
		for _, story := range []string{"default", "loading", "empty", "failed", "refused", "offline", "offline-failed", "zero", "included-tax", "discount", "fee", "incomplete", "stale"} {
			p := sharedSummary(w)
			p.ComponentProps = sharedProps("summary", w)
			p.State = sharedState(story, w)
			if absent(p.State) {
				p.Lines = nil
				p.Subtotal = nil
				p.Total = nil
				p.Complete = false
			} else if story == "incomplete" || story == "stale" {
				p.Complete = false
				p.Total = nil
				p.IncompleteText = w.stale
			} else if story == "zero" {
				p.Lines = nil
				p.Subtotal = new(sharedMoney(0, w))
				p.Total = new(sharedMoney(0, w))
			}
			entries = append(entries, ExampleOf(sharedInfo("order-summary", story, w), p, c.OrderSummary))
		}
		for _, story := range []string{"default", "loading", "empty", "failed", "refused", "offline", "offline-failed", "updating", "removed-last-line", "sold-out-line", "stale-quote", "incomplete-quote", "uncertain-write"} {
			p := c.CartProps{ComponentProps: sharedProps("cart", w), Label: w.summary, State: sharedState(story, w)}
			slots := c.CartSlots{}
			if story == "removed-last-line" {
				p.State = sharedState("empty", w)
			}
			if !absent(p.State) {
				p.ResultKey = "quote-one"
				p.QuoteState = "current"
				p.QuoteText = w.details
				p.Summary = sharedSummary(w)
				for i, unit := range []int64{125, 200} {
					quantity := int64(1)
					if i == 0 {
						quantity = 2
					}
					q := sharedQuantity(w)
					q.ID = fmt.Sprintf("line-%d-%s", i, w.locale)
					q.Attrs = nil
					q.Min = 1
					q.Step = 1
					q.Value = quantity
					q.FormID = q.ID + "-form"
					p.Lines = append(p.Lines, c.CartLine{ID: fmt.Sprint(i), ProductID: fmt.Sprint(i), Title: w.caption + " " + fmt.Sprint(i+1), UnitPrice: sharedMoney(unit, w), Quantity: quantity, QuantityInput: q, Availability: "available", AvailabilityText: w.available, LineTotal: sharedMoney(unit*quantity, w), Revision: "1"})
				}
				if story == "updating" {
					p.Pending = true
					p.QuoteText = w.loading
				}
				if story == "sold-out-line" {
					p.Lines[0].Availability = "sold-out"
					p.Lines[0].AvailabilityText = w.unavailable
				}
				if story == "stale-quote" || story == "uncertain-write" {
					p.QuoteState = "stale"
					p.QuoteText = w.stale
				}
				if story == "incomplete-quote" {
					p.QuoteState = "incomplete"
					p.Summary.Complete = false
					p.Summary.Total = nil
					p.Summary.IncompleteText = w.unknown
					p.QuoteText = w.unknown
				}
				slots.LineActions = func(line c.CartLine) g.Node {
					return c.Form(c.FormProps{ComponentProps: c.ComponentProps{ID: line.QuantityInput.FormID}, Action: "/cart/" + line.ID}, c.Button(c.ButtonProps{Label: w.save, Variant: "outline", Type: "submit", Size: "lg"}))
				}
				slots.Checkout = []g.Node{c.Button(c.ButtonProps{Label: w.continueText, Href: "/checkout", Size: "lg"})}
			}
			entries = append(entries, ExampleWithSlots(sharedInfo("cart", story, w), p, slots, c.CartWithSlots))
		}
	}
	return entries
}

func sharedPlans(w sharedCopy) []c.Plan {
	return []c.Plan{
		{ID: "one", Name: w.plan + " A", Price: new(sharedMoney(1200, w)), PeriodText: w.period, Features: []c.PlanFeature{{Key: "access", Label: w.label, State: "included", Text: w.included}}, Action: c.ButtonProps{Label: w.continueText, Href: "/plans/one"}},
		{ID: "two", Name: w.plan + " B", Price: new(sharedMoney(2400, w)), PeriodText: w.period, Recommended: true, BadgeText: w.recommended, Features: []c.PlanFeature{{Key: "access", Label: w.label, State: "value", Text: "2"}}, Action: c.ButtonProps{Label: w.continueText, Href: "/plans/two"}},
	}
}
func planExamples() []Example {
	var entries []Example
	for _, w := range sharedLocales() {
		for _, story := range []string{"default", "loading", "empty", "failed", "refused", "offline", "offline-failed", "current", "recommended", "period-selected", "unavailable", "unknown-price", "pending", "unknown-feature", "long-features", "narrow"} {
			p := c.PricingTiersProps{ComponentProps: sharedProps("plans", w), Label: w.plan, State: sharedState(story, w)}
			if !absent(p.State) {
				p.Plans = sharedPlans(w)
				p.CurrentPlanID = "one"
				p.CurrentPlanText = w.current
				p.Periods = []c.ChoiceLink{{Key: "month", Label: w.period, Href: "/plans?period=month"}}
				p.SelectedPeriod = "month"
				switch story {
				case "unavailable":
					p.Plans[1].Unavailable = true
					p.Plans[1].UnavailableText = w.unavailable
				case "unknown-price":
					p.Plans[1].Price = nil
					p.Plans[1].PriceText = w.unknown
				case "pending":
					p.Plans[1].Action.Loading = true
				case "unknown-feature":
					p.Plans[1].Features[0].State = "unknown"
					p.Plans[1].Features[0].Text = w.unknown
				case "long-features":
					p.Plans[1].Features[0].Text = w.details
				}
			}
			entries = append(entries, ExampleOf(sharedInfo("pricing-tiers", story, w), p, c.PricingTiers))
			comparison := c.PlanComparisonProps{ComponentProps: sharedProps("comparison", w), Label: w.plan, State: p.State, Plans: p.Plans, CurrentPlanID: p.CurrentPlanID, CurrentPlanText: p.CurrentPlanText, Caption: w.details}
			if !absent(p.State) {
				comparison.Features = []c.FeatureHeading{{Key: "access", Label: w.label}}
			}
			entries = append(entries, ExampleOf(sharedInfo("plan-comparison", story, w), comparison, c.PlanComparison))
		}
	}
	return entries
}

func calendarExamples() []Example {
	var entries []Example
	for _, w := range sharedLocales() {
		for _, story := range []string{"default", "loading", "empty", "failed", "refused", "offline", "offline-failed", "day", "week", "overlap", "overnight", "dst-fold", "all-day", "long-event", "selected", "stale-range"} {
			p := c.CalendarProps{ComponentProps: sharedProps("calendar", w), Label: w.label, State: sharedState(story, w)}
			if !absent(p.State) {
				p.View = "agenda"
				p.DateStrip = sharedDates(w)
				p.RangeStartDate = "2026-10-24"
				p.RangeEndDate = "2026-10-27"
				p.TimeZone = "Europe/Lisbon"
				p.TimeZoneLabel = w.timeZone
				p.Language = w.locale
				p.FirstWeekday = 1
				p.NowUTC = time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
				p.AllDayLabel = map[string]string{"en": "All day", "pt-PT": "Todo o dia"}[w.locale]
				p.AgendaLabel = w.details
				p.GridLabel = w.label
				p.FallbackText = w.offline
				p.Views = []c.ChoiceLink{{Key: "agenda", Label: w.details, Href: "/calendar?view=agenda", Selected: true}, {Key: "day", Label: "25", Href: "/calendar?view=day"}, {Key: "week", Label: "24–26", Href: "/calendar?view=week"}}
				p.Previous = &c.ChoiceLink{Key: "previous", Label: w.previous, Href: "/calendar?date=2026-10-23"}
				p.Next = &c.ChoiceLink{Key: "next", Label: w.next, Href: "/calendar?date=2026-10-27"}
				p.Events = []c.CalendarEvent{{ID: "early", Title: w.caption, Description: w.details, TimeText: "01:30 UTC+01:00", StatusLabel: w.available, StartUTC: time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC), EndUTC: time.Date(2026, 10, 25, 1, 15, 0, 0, time.UTC), Href: "/events/early"}, {ID: "late", Title: w.label, TimeText: "01:30 UTC+00:00", StatusLabel: w.booked, Tone: "warning", StartUTC: time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC), EndUTC: time.Date(2026, 10, 25, 2, 15, 0, 0, time.UTC), Href: "/events/late"}}
				switch story {
				case "day", "week":
					p.View = story
				case "overlap":
					p.View = "day"
					p.Events[1].StartUTC = p.Events[0].StartUTC
				case "overnight":
					p.Events[0].StartUTC = time.Date(2026, 10, 24, 22, 30, 0, 0, time.UTC)
				case "all-day":
					p.Events = append(p.Events, c.CalendarEvent{ID: "all", Title: w.label, TimeText: "24–25", StatusLabel: w.available, AllDay: true, StartDate: "2026-10-24", EndDate: "2026-10-26"})
				case "long-event":
					p.Events[0].Title = w.details + " " + w.details
				case "selected":
					p.SelectedID = "early"
				case "stale-range":
					p.State.Offline = true
					p.State.OfflineText = w.stale
				}
			}
			entries = append(entries, ExampleOf(sharedInfo("calendar", story, w), p, c.Calendar))
		}
	}
	return entries
}
func mapExamples() []Example {
	var entries []Example
	for _, w := range sharedLocales() {
		for _, story := range []string{"default", "loading", "empty", "failed", "refused", "offline", "offline-failed", "map", "tile-failure", "selected", "removed-selection", "many-points", "antimeridian", "polar"} {
			p := c.MapViewProps{ComponentProps: sharedProps("map", w), Label: w.label, State: sharedState(story, w)}
			if !absent(p.State) {
				p.View = "list"
				p.MapLabel = w.label
				p.ListLabel = w.details
				p.StatusLabel = w.status
				p.MapUnavailableText = w.offline
				p.ZoomInLabel = w.next
				p.ZoomOutLabel = w.previous
				p.SnapshotText = w.details
				p.Viewport = c.MapViewport{Latitude: 38.72, Longitude: -9.14, Zoom: 12}
				p.Legend = []c.MapLegend{{Key: "available", Label: w.available, Symbol: "circle", Tone: "success"}, {Key: "unavailable", Label: w.unavailable, Symbol: "square", Tone: "warning"}}
				p.Views = []c.ChoiceLink{{Key: "list", Label: w.details, Href: "/places?view=list", Selected: story == "default"}, {Key: "map", Label: w.label, Href: "/places?view=map", Selected: story == "map"}}
				p.Points = []c.MapPoint{{ID: "one", Title: w.caption, Description: w.details, Latitude: 38.72, Longitude: -9.14, StatusKey: "available", StatusText: w.available, Href: "/places/one"}, {ID: "two", Title: w.label, Latitude: 38.73, Longitude: -9.15, StatusKey: "unavailable", StatusText: w.unavailable, Href: "/places/two"}}
				if story == "map" || story == "tile-failure" {
					p.View = "map"
					p.Tiles = &c.MapTiles{URLTemplate: "/example-tiles/{z}/{x}/{y}.png", MinZoom: 0, MaxZoom: 18, Attributions: []c.Attribution{{Text: w.caption, Href: "/map-attribution"}}}
				}
				switch story {
				case "selected":
					p.SelectedID = "one"
				case "removed-selection":
					p.SelectedID = "removed"
				case "many-points":
					for i := 0; i < 20; i++ {
						point := p.Points[0]
						point.ID = fmt.Sprint(i)
						point.Title = w.label + " " + point.ID
						point.Latitude += float64(i) / 1000
						p.Points = append(p.Points, point)
					}
				case "antimeridian":
					p.Points[0].Longitude = 179.9
					p.Points[1].Longitude = -179.9
				case "polar":
					p.View = "map"
					p.Points[0].Latitude = 89.9
				}
			}
			entries = append(entries, ExampleOf(sharedInfo("map-view", story, w), p, c.MapView))
		}
	}
	return entries
}
func mediaExamples() []Example {
	var entries []Example
	for _, w := range sharedLocales() {
		for _, story := range []string{"default", "loading", "empty", "failed", "refused", "offline", "offline-failed", "one", "mixed-aspects", "long-caption", "image-failed", "image-refused", "viewer-open", "removed-selection", "load-more"} {
			p := c.PhotoGalleryProps{ComponentProps: sharedProps("photos", w), Label: w.label, State: sharedState(story, w), PreviousLabel: w.previous, NextLabel: w.next, CloseLabel: w.close, ReturnHref: "/photos"}
			if !absent(p.State) {
				p.Items = []c.Photo{sharedPhoto("one", w), sharedPhoto("two", w), sharedPhoto("three", w)}
				switch story {
				case "one":
					p.Items = p.Items[:1]
				case "mixed-aspects":
					p.Items[1].Media.Width = 90
					p.Items[1].Media.Height = 160
					p.Items[1].Full = p.Items[1].Media
				case "long-caption":
					p.Items[0].Media.Caption = w.details + " " + w.details
				case "image-failed", "image-refused":
					status := c.MediaFailed
					if story == "image-refused" {
						status = c.MediaRefused
					}
					p.Items[1].Media = c.MediaProps{Status: status, Reason: w.failed}
					p.Items[1].Full = p.Items[1].Media
				case "viewer-open":
					p.SelectedID = "two"
					p.ViewerOpen = true
				case "removed-selection":
					p.SelectedID = "removed"
					p.ViewerOpen = true
				}
			}
			entries = append(entries, ExampleOf(sharedInfo("photo-gallery", story, w), p, c.PhotoGallery))
			masonry := c.MasonryProps{ComponentProps: sharedProps("masonry", w), Label: w.label, State: p.State, Items: p.Items, Columns: 1, SMColumns: 2, LGColumns: 3, Gap: "4"}
			if story == "load-more" {
				masonry.More = &c.ChoiceLink{Key: "more", Label: w.next, Href: "/photos?page=2"}
			}
			entries = append(entries, ExampleOf(sharedInfo("masonry", story, w), masonry, c.Masonry))
		}
		entries = append(entries, ExampleWithSlots(sharedInfo("hero", "full-bleed", w), c.HeroProps{SectionHeaderProps: c.SectionHeaderProps{Title: w.label, Description: w.details}, Layout: "full-bleed"}, c.HeroSlots{Media: []g.Node{c.Media(sharedPhoto("hero", w).Media)}, Actions: []g.Node{c.Button(c.ButtonProps{Label: w.continueText, Href: "/items", Size: "lg"})}}, c.Hero))
	}
	return entries
}
func chartExamples() []Example {
	var entries []Example
	for _, w := range sharedLocales() {
		for _, story := range []string{"default", "loading", "empty", "failed", "refused", "offline", "offline-failed", "zero", "negative", "constant", "single-point", "gaps", "multiple-series", "long-label", "unknown-comparison"} {
			state := sharedState(story, w)
			points := []c.ChartPoint{{Key: "one", X: 0, Y: new(2.0), XText: "A", YText: "2"}, {Key: "two", X: 1, Y: new(4.0), XText: "B", YText: "4"}, {Key: "three", X: 2, Y: new(3.0), XText: "C", YText: "3"}}
			switch story {
			case "zero":
				for i := range points {
					points[i].Y = new(0.0)
					points[i].YText = "0"
				}
			case "negative":
				points[0].Y = new(-2.0)
				points[0].YText = "−2"
			case "constant":
				for i := range points {
					points[i].Y = new(2.0)
					points[i].YText = "2"
				}
			case "single-point":
				points = points[:1]
			case "gaps":
				points[1].Y = nil
				points[1].YText = w.unknown
			case "long-label":
				points[0].XText = w.details
			}
			spark := c.SparklineProps{ComponentProps: sharedProps("sparkline", w), Label: w.label, State: state}
			area := c.AreaChartProps{ComponentProps: sharedProps("area", w), Label: w.label, State: state, XLabel: w.label, YLabel: w.quantity, TableLabel: w.details, ShowDataLabel: w.showData}
			bars := c.BarChartProps{ComponentProps: sharedProps("bars", w), Label: w.label, State: state, AxisLabel: w.quantity, TableLabel: w.details, ShowDataLabel: w.showData}
			stat := c.StatTileProps{ComponentProps: sharedProps("stat", w), Label: w.label, State: state}
			if !absent(state) {
				spark.Points = points
				spark.Summary = w.details
				area.Description = w.details
				area.Series = []c.ChartSeries{{Key: "first", Label: w.label, Points: points, Tone: "info"}}
				area.XTicks = []c.Tick{{Value: 0, Text: "A"}, {Value: 1, Text: "B"}, {Value: 2, Text: "C"}}
				area.YTicks = []c.Tick{{Value: 0, Text: "0"}, {Value: 4, Text: "4"}}
				bars.Description = w.details
				bars.Ticks = []c.Tick{{Value: 0, Text: "0"}, {Value: 4, Text: "4"}}
				for _, point := range points {
					if point.Y != nil {
						bars.Bars = append(bars.Bars, c.Bar{Key: point.Key, Label: point.XText, Value: *point.Y, ValueText: point.YText, Tone: "info"})
					}
				}
				stat.ValueText = "9"
				stat.Description = w.details
				stat.DeltaDirection = "up"
				stat.DeltaTone = "success"
				stat.DeltaText = "+2"
				stat.ComparisonText = w.caption
				if story == "multiple-series" {
					second := append([]c.ChartPoint(nil), points...)
					for i := range second {
						second[i].Y = new(*second[i].Y + 1)
						second[i].YText = fmt.Sprint(*second[i].Y)
					}
					area.Series = append(area.Series, c.ChartSeries{Key: "second", Label: w.status, Points: second, Tone: "warning", Pattern: "dash"})
				}
				if story == "unknown-comparison" {
					stat.DeltaDirection = "unknown"
					stat.DeltaTone = "neutral"
					stat.DeltaText = w.unknown
				}
				if story == "negative" {
					stat.DeltaDirection = "down"
					stat.DeltaTone = "danger"
					stat.DeltaText = "−2"
				}
				if story == "zero" {
					stat.ValueText = "0"
					stat.DeltaDirection = "flat"
					stat.DeltaText = "0"
				}
			}
			if !absent(state) {
				b, _, _ := c.ChartRange(points, true)
				area.XTicks = []c.Tick{{Value: b.MinX, Text: points[0].XText}, {Value: b.MaxX, Text: points[len(points)-1].XText}}
				area.YTicks = []c.Tick{{Value: b.MinY, Text: fmt.Sprint(b.MinY)}, {Value: b.MaxY, Text: fmt.Sprint(b.MaxY)}}
				bars.Ticks = area.YTicks
			}
			entries = append(entries, ExampleOf(sharedInfo("sparkline", story, w), spark, c.Sparkline), ExampleOf(sharedInfo("area-chart", story, w), area, c.AreaChart), ExampleOf(sharedInfo("bar-chart", story, w), bars, c.BarChart), ExampleOf(sharedInfo("stat-tile", story, w), stat, c.StatTile))
		}
	}
	return entries
}
