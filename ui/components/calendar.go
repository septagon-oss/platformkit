package components

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

type CalendarEvent struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	TimeText    string    `json:"timeText"`
	StatusLabel string    `json:"statusLabel"`
	Tone        string    `json:"tone,omitempty"`
	Href        string    `json:"href,omitempty"`
	AllDay      bool      `json:"allDay,omitzero"`
	StartUTC    time.Time `json:"startUTC,omitzero"`
	EndUTC      time.Time `json:"endUTC,omitzero"`
	StartDate   string    `json:"startDate,omitempty"`
	EndDate     string    `json:"endDate,omitempty"`
}
type CalendarProps struct {
	ComponentProps
	Label          string          `json:"label"`
	State          ContentState    `json:"state,omitzero"`
	View           string          `json:"view,omitempty" enum:",day,week,agenda"`
	DateStrip      DateStripProps  `json:"dateStrip"`
	RangeStartDate string          `json:"rangeStartDate"`
	RangeEndDate   string          `json:"rangeEndDate"`
	TimeZone       string          `json:"timeZone"`
	TimeZoneLabel  string          `json:"timeZoneLabel"`
	Language       string          `json:"language"`
	FirstWeekday   int             `json:"firstWeekday"`
	NowUTC         time.Time       `json:"nowUTC"`
	Events         []CalendarEvent `json:"events,omitempty"`
	Views          []ChoiceLink    `json:"views,omitempty"`
	Previous       *ChoiceLink     `json:"previous,omitempty"`
	Next           *ChoiceLink     `json:"next,omitempty"`
	Today          *ChoiceLink     `json:"today,omitempty"`
	AllDayLabel    string          `json:"allDayLabel"`
	AgendaLabel    string          `json:"agendaLabel"`
	GridLabel      string          `json:"gridLabel"`
	FallbackText   string          `json:"fallbackText"`
	SelectedID     string          `json:"selectedID,omitempty"`
}
type CalendarSlots struct {
	StateSlots
	EventDetails func(CalendarEvent) g.Node
}
type CalendarDay struct {
	Date   string
	Events []CalendarEvent
}

func (p CalendarProps) Validate() error {
	if err := aggregateState(p.Label, p.State, len(p.Events) > 0 || p.SelectedID != ""); err != nil {
		return err
	}
	if !p.State.ready() {
		return nil
	}
	if !required(p.ID, p.TimeZone, p.TimeZoneLabel, p.Language, p.AllDayLabel, p.AgendaLabel, p.GridLabel, p.FallbackText) || !utcInstant(p.NowUTC) || p.FirstWeekday < 0 || p.FirstWeekday > 6 {
		return fmt.Errorf("Calendar: labels, explicit locale/zone/now and weekday required")
	}
	if p.View != "" && p.View != "agenda" && p.View != "day" && p.View != "week" {
		return fmt.Errorf("Calendar: unknown view")
	}
	if _, err := time.LoadLocation(p.TimeZone); err != nil || p.TimeZone == "Local" {
		return fmt.Errorf("Calendar: explicit IANA zone required")
	}
	start, err := civilDate(p.RangeStartDate)
	if err != nil {
		return err
	}
	end, err := civilDate(p.RangeEndDate)
	if err != nil || !end.After(start) || end.Sub(start) > 366*24*time.Hour {
		return fmt.Errorf("Calendar: ordered range of at most 366 civil days required")
	}
	if err := p.DateStrip.Validate(); err != nil {
		return err
	}
	// The strip marks its selected day aria-current and the grid opens on it, so
	// a day outside [start, end) is a detail from a range no longer displayed.
	if p.DateStrip.SelectedDate < p.RangeStartDate || p.DateStrip.SelectedDate >= p.RangeEndDate {
		return fmt.Errorf("Calendar: selected date must fall inside the displayed range")
	}
	ids := map[string]bool{}
	for _, event := range p.Events {
		if !required(event.ID, event.Title, event.TimeText, event.StatusLabel) || ids[event.ID] {
			return fmt.Errorf("Calendar: unique labeled events required")
		}
		ids[event.ID] = true
		if event.AllDay {
			a, err := civilDate(event.StartDate)
			if err != nil {
				return err
			}
			b, err := civilDate(event.EndDate)
			if err != nil || !b.After(a) || !event.StartUTC.IsZero() || !event.EndUTC.IsZero() {
				return fmt.Errorf("Calendar: all-day events use exclusive civil dates only")
			}
		} else if event.StartDate != "" || event.EndDate != "" || !utcInstant(event.StartUTC) || !utcInstant(event.EndUTC) || !event.EndUTC.After(event.StartUTC) {
			return fmt.Errorf("Calendar: timed events require ordered UTC instants only")
		}
	}
	if err := validateChoices(p.Views); err != nil {
		return err
	}
	for _, choice := range []*ChoiceLink{p.Previous, p.Next, p.Today} {
		if choice != nil {
			if err := validateChoices([]ChoiceLink{*choice}); err != nil {
				return err
			}
		}
	}
	return nil
}

// CalendarAgenda enumerates civil dates independently of zone transitions, then
// intersects half-open intervals with their local boundaries. Skipped dates
// refuse the whole range; ordinary 23/25-hour days keep their actual duration.
func CalendarAgenda(p CalendarProps) ([]CalendarDay, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if !p.State.ready() {
		return nil, nil
	}
	zone, _ := time.LoadLocation(p.TimeZone)
	start, _ := civilDate(p.RangeStartDate)
	end, _ := civilDate(p.RangeEndDate)
	var days []CalendarDay
	for date := start; date.Before(end); date = date.AddDate(0, 0, 1) {
		entry := CalendarDay{Date: date.Format(time.DateOnly)}
		nextDate := date.AddDate(0, 0, 1).Format(time.DateOnly)
		day, _ := time.ParseInLocation(time.DateOnly, entry.Date, zone)
		next, _ := time.ParseInLocation(time.DateOnly, nextDate, zone)
		if day.Format(time.DateOnly) != entry.Date || next.Format(time.DateOnly) != nextDate || !next.After(day) {
			return nil, fmt.Errorf("Calendar: civil day %s has no ordered boundaries in %s", entry.Date, p.TimeZone)
		}
		for _, event := range p.Events {
			overlaps := event.StartUTC.Before(next) && event.EndUTC.After(day)
			if event.AllDay {
				overlaps = event.StartDate < nextDate && event.EndDate > entry.Date
			}
			if overlaps {
				entry.Events = append(entry.Events, event)
			}
		}
		slices.SortStableFunc(entry.Events, func(a, b CalendarEvent) int {
			if a.AllDay != b.AllDay {
				if a.AllDay {
					return -1
				}
				return 1
			}
			if a.AllDay {
				return cmp.Or(cmp.Compare(a.StartDate, b.StartDate), cmp.Compare(a.ID, b.ID))
			}
			return cmp.Or(a.StartUTC.Compare(b.StartUTC), cmp.Compare(a.ID, b.ID))
		})
		days = append(days, entry)
	}
	return days, nil
}
func Calendar(p CalendarProps) g.Node { return CalendarWithSlots(p, CalendarSlots{}) }
func CalendarWithSlots(p CalendarProps, slots CalendarSlots) g.Node {
	if err := p.Validate(); err != nil {
		return invalidComponent(err)
	}
	if !p.State.ready() {
		return sharedSection(p.ComponentProps, "calendar", p.Label, stateBody(p.State, slots.StateSlots))
	}
	days, err := CalendarAgenda(p)
	if err != nil {
		return invalidComponent(err)
	}
	var agenda []g.Node
	for _, day := range days {
		if len(day.Events) == 0 {
			continue
		}
		label := day.Date
		for _, choice := range p.DateStrip.Days {
			if choice.Date == day.Date {
				label = choice.Label
			}
		}
		var events []g.Node
		for _, event := range day.Events {
			var title g.Node = Heading(HeadingProps{Text: event.Title, Level: 3, Size: 5})
			if event.Href != "" {
				title = recoveryAction(ButtonProps{Label: event.Title, Href: event.Href, Variant: "link"}, p.Disabled)
			}
			var detail g.Node
			if slots.EventDetails != nil {
				detail = slots.EventDetails(event)
			}
			timeText := event.TimeText
			if event.AllDay {
				timeText = p.AllDayLabel + " · " + timeText
			}
			events = append(events, h.Li(g.Attr("data-calendar-event", event.ID), g.If(event.ID == p.SelectedID, g.Attr("aria-current", "true")), title, Text(TextProps{Content: timeText, Size: "sm"}), Badge(BadgeProps{Label: event.StatusLabel, Tone: event.Tone}), Text(TextProps{Content: event.Description, Size: "sm"}), detail))
		}
		agenda = append(agenda, h.Li(Heading(HeadingProps{Text: label, Level: 2, Size: 4}), h.Ul(h.Class(clDataList.Compile()), g.Group(events))))
	}
	var controls []g.Node
	for _, choice := range []*ChoiceLink{p.Previous, p.Today, p.Next} {
		if choice != nil {
			controls = append(controls, choiceLinks([]ChoiceLink{*choice}, p.Disabled))
		}
	}
	view := cmp.Or(p.View, "agenda")
	views := slices.Clone(p.Views)
	for i := range views {
		views[i].Selected = views[i].Key == view
	}
	var enhancement g.Node
	if view != "agenda" {
		var events []map[string]any
		for _, event := range p.Events {
			start, end := event.StartDate, event.EndDate
			if !event.AllDay {
				start = event.StartUTC.Format(time.RFC3339Nano)
				end = event.EndUTC.Format(time.RFC3339Nano)
			}
			events = append(events, map[string]any{"id": event.ID, "title": event.Title, "start": start, "end": end, "allDay": event.AllDay, "url": event.Href, "extendedProps": map[string]string{"timeText": event.TimeText, "statusLabel": event.StatusLabel}})
		}
		config, _ := json.Marshal(map[string]any{"view": view, "date": p.DateStrip.SelectedDate, "start": p.RangeStartDate, "end": p.RangeEndDate, "zone": p.TimeZone, "locale": p.Language, "firstDay": p.FirstWeekday, "now": p.NowUTC.Format(time.RFC3339Nano), "allDay": p.AllDayLabel, "events": events})
		enhancement = h.Details(h.Open(), h.Summary(h.Class(clDataDisclosure.Compile()), g.Text(p.GridLabel)), h.Div(g.Attr("data-calendar-config", string(config)), g.Attr("data-calendar-engine", ""), h.Role("region"), g.Attr("aria-label", p.GridLabel), g.Attr("tabindex", "0")), h.P(g.Attr("data-calendar-fallback", ""), g.Text(p.FallbackText)))
	}
	return sharedSection(p.ComponentProps, "calendar", p.Label, stateBody(p.State, slots.StateSlots), DateStrip(p.DateStrip), Flex(FlexProps{Wrap: true, Gap: "2"}, controls...), choiceLinks(views, p.Disabled), Text(TextProps{Content: p.TimeZoneLabel, Size: "sm"}), enhancement, h.Section(g.Attr("aria-label", p.AgendaLabel), h.Ol(h.Class(clDataList.Compile()), g.Group(agenda))))
}
