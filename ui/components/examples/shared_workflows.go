package examples

import (
	"time"

	g "maragu.dev/gomponents"

	c "github.com/septagon-oss/platformkit/ui/components"
)

func stepperExamplesForLocale(w sharedCopy) []Example {
	var entries []Example
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

	return entries
}

func dateStripExamplesForLocale(w sharedCopy) []Example {
	var entries []Example
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

	return entries
}

func slotPickerExamplesForLocale(w sharedCopy) []Example {
	var entries []Example
	for _, story := range []string{"default", "loading", "empty", "failed", "refused", "offline", "offline-failed", "selected", "booked", "full", "capacity-unknown", "unavailable", "past", "overnight", "dst-fold", "stale", "invalid-selection", "confirmation-pending"} {
		p := c.SlotPickerProps{ComponentProps: sharedProps("slots", w), Label: w.label, State: sharedState(story, w), DateStrip: sharedDates(w), TimeZone: "Europe/Lisbon", TimeZoneLabel: w.timeZone, NowUTC: time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC), Name: "slot", Quantity: 1, SnapshotText: w.details, StatusLabels: c.SlotStatusLabels{Available: w.available, Booked: w.booked, Past: w.past, Unavailable: w.unavailable, Full: w.full, Stale: w.stale, CapacityUnknown: w.unknown}}
		p.Slots = []c.Slot{{ID: "early", StartUTC: time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC), EndUTC: time.Date(2026, 10, 25, 1, 0, 0, 0, time.UTC), TimeText: "01:30 UTC+01:00", Availability: "available", Capacity: new(int64(3)), Remaining: new(int64(1)), CapacityText: "1 / 3"}, {ID: "late", StartUTC: time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC), EndUTC: time.Date(2026, 10, 25, 2, 0, 0, 0, time.UTC), TimeText: "01:30 UTC+00:00", Availability: "available"}}
		if absent(p.State) {
			p.Slots = nil
			if p.State.Status == c.MediaFailed || p.State.Status == c.MediaRefused {
				p.DateStrip = c.DateStripProps{}
			}
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
	return entries
}
