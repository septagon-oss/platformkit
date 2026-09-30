package components

import (
	"fmt"
	"strings"
	"time"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

type DateChoice struct {
	Date     string `json:"date"`
	Label    string `json:"label"`
	Href     string `json:"href,omitempty"`
	Today    bool   `json:"today,omitzero"`
	Disabled bool   `json:"disabled,omitzero"`
	Reason   string `json:"reason,omitempty"`
}

type DateStripProps struct {
	ComponentProps
	Label        string       `json:"label"`
	Days         []DateChoice `json:"days"`
	SelectedDate string       `json:"selectedDate"`
	TodayText    string       `json:"todayText,omitempty"`
	Previous     *ChoiceLink  `json:"previous,omitempty"`
	Next         *ChoiceLink  `json:"next,omitempty"`
}

func civilDate(value string) (time.Time, error) { return time.Parse(time.DateOnly, value) }

func (p DateStripProps) Validate() error {
	if !required(p.Label) || len(p.Days) == 0 {
		return fmt.Errorf("DateStrip: Label and days are required")
	}
	seen, selected := map[string]bool{}, false
	for _, day := range p.Days {
		if _, err := civilDate(day.Date); err != nil {
			return fmt.Errorf("DateStrip: invalid civil date")
		}
		if day.Today && !required(p.TodayText) {
			return fmt.Errorf("DateStrip: today needs localized copy")
		}
		if seen[day.Date] || !required(day.Label) || !day.Disabled && !required(day.Href) {
			return fmt.Errorf("DateStrip: unique dates, labels and enabled URLs are required")
		}
		seen[day.Date] = true
		if day.Date == p.SelectedDate {
			selected = !day.Disabled
		}
	}
	if !selected {
		return fmt.Errorf("DateStrip: selected date must be an enabled supplied day")
	}
	for _, choice := range []*ChoiceLink{p.Previous, p.Next} {
		if choice != nil {
			if err := validateChoices([]ChoiceLink{*choice}); err != nil {
				return err
			}
		}
	}
	return nil
}

func DateStrip(p DateStripProps) g.Node {
	if err := p.Validate(); err != nil {
		return invalidComponent(err)
	}
	var days []g.Node
	if p.Previous != nil {
		days = append(days, choiceLinks([]ChoiceLink{*p.Previous}, p.Disabled))
	}
	for _, day := range p.Days {
		attrs := map[string]string{}
		if day.Date == p.SelectedDate {
			attrs["aria-current"] = "date"
		}
		if day.Today {
			attrs["data-today"] = "true"
		}
		label := day.Label
		if day.Today {
			label += " · " + p.TodayText
		}
		if day.Reason != "" {
			label += " · " + day.Reason
		}
		days = append(days, recoveryAction(ButtonProps{ComponentProps: ComponentProps{Disabled: p.Disabled || day.Disabled, Attrs: attrs}, Label: label, Href: day.Href, Variant: "outline"}, false))
	}
	if p.Next != nil {
		days = append(days, choiceLinks([]ChoiceLink{*p.Next}, p.Disabled))
	}
	return h.Nav(append(baseAttrs(p.ComponentProps), g.Attr("data-component", "date-strip"), g.Attr("aria-label", p.Label), Flex(FlexProps{Gap: "2", Wrap: true}, days...))...)
}

type Slot struct {
	ID           string    `json:"id"`
	StartUTC     time.Time `json:"startUTC"`
	EndUTC       time.Time `json:"endUTC"`
	TimeText     string    `json:"timeText"`
	Availability string    `json:"availability" enum:"available,unavailable,booked"`
	Capacity     *int64    `json:"capacity,omitempty,string"`
	Remaining    *int64    `json:"remaining,omitempty,string"`
	CapacityText string    `json:"capacityText,omitempty"`
	Reason       string    `json:"reason,omitempty"`
}

type SlotStatusLabels struct {
	Available       string `json:"available"`
	Booked          string `json:"booked"`
	Past            string `json:"past"`
	Unavailable     string `json:"unavailable"`
	Full            string `json:"full"`
	Stale           string `json:"stale"`
	CapacityUnknown string `json:"capacityUnknown"`
}

type SlotPickerProps struct {
	ComponentProps
	Label         string           `json:"label"`
	State         ContentState     `json:"state,omitzero"`
	DateStrip     DateStripProps   `json:"dateStrip"`
	TimeZone      string           `json:"timeZone"`
	TimeZoneLabel string           `json:"timeZoneLabel"`
	NowUTC        time.Time        `json:"nowUTC"`
	Slots         []Slot           `json:"slots,omitempty"`
	SelectedID    string           `json:"selectedID,omitempty"`
	Quantity      int64            `json:"quantity,string"`
	Name          string           `json:"name"`
	FormID        string           `json:"formID,omitempty"`
	Required      bool             `json:"required,omitzero"`
	ErrorText     string           `json:"errorText,omitempty"`
	SnapshotText  string           `json:"snapshotText"`
	Stale         bool             `json:"stale,omitzero"`
	StatusLabels  SlotStatusLabels `json:"statusLabels"`
}

func utcInstant(t time.Time) bool {
	return !t.IsZero() && t.Location() == time.UTC && t.Year() >= 1 && t.Year() <= 9999
}

// SlotEligibility evaluates a supplied snapshot. It does not reserve capacity.
func SlotEligibility(slot Slot, now time.Time, quantity int64, stale bool) (string, error) {
	if !utcInstant(now) || !utcInstant(slot.StartUTC) || !utcInstant(slot.EndUTC) || !slot.EndUTC.After(slot.StartUTC) || quantity <= 0 {
		return "", fmt.Errorf("Slot: positive quantity and ordered UTC interval required")
	}
	if (slot.Capacity == nil) != (slot.Remaining == nil) || slot.Capacity != nil && (*slot.Capacity < 0 || *slot.Remaining < 0 || *slot.Remaining > *slot.Capacity) {
		return "", fmt.Errorf("Slot: invalid capacity")
	}
	switch slot.Availability {
	case "available", "booked", "unavailable":
	default:
		return "", fmt.Errorf("Slot: unknown availability")
	}
	switch {
	case stale:
		return "stale", nil
	case slot.Availability == "booked":
		return "booked", nil
	case !slot.StartUTC.After(now):
		return "past", nil
	case slot.Availability == "unavailable":
		return "unavailable", nil
	case slot.Remaining != nil && *slot.Remaining < quantity:
		return "full", nil
	default:
		return "available", nil
	}
}

func (p SlotPickerProps) Validate() error {
	if err := aggregateState(p.Label, p.State, len(p.Slots) > 0 || p.SelectedID != ""); err != nil {
		return err
	}
	if !p.State.ready() {
		return nil
	}
	if !required(p.ID, p.Name, p.TimeZone, p.TimeZoneLabel, p.SnapshotText) || p.Quantity <= 0 || !utcInstant(p.NowUTC) {
		return fmt.Errorf("SlotPicker: identity, labels, positive quantity and explicit UTC now required")
	}
	zone, err := time.LoadLocation(p.TimeZone)
	if err != nil || p.TimeZone == "Local" {
		return fmt.Errorf("SlotPicker: explicit IANA zone required")
	}
	if err := p.DateStrip.Validate(); err != nil {
		return err
	}
	labels := p.StatusLabels
	if !required(labels.Available, labels.Booked, labels.Past, labels.Unavailable, labels.Full, labels.Stale, labels.CapacityUnknown) {
		return fmt.Errorf("SlotPicker: localized status labels required")
	}
	ids, validSelection := map[string]bool{}, p.SelectedID == ""
	for _, slot := range p.Slots {
		if !required(slot.ID, slot.TimeText) || ids[slot.ID] {
			return fmt.Errorf("SlotPicker: unique IDs and time labels required")
		}
		ids[slot.ID] = true
		status, err := SlotEligibility(slot, p.NowUTC, p.Quantity, p.Stale)
		if err != nil {
			return err
		}
		if slot.StartUTC.In(zone).Format(time.DateOnly) != p.DateStrip.SelectedDate {
			return fmt.Errorf("SlotPicker: slot starts outside selected local date")
		}
		if slot.ID == p.SelectedID && status == "available" {
			validSelection = true
		}
	}
	if !validSelection && !required(p.ErrorText) {
		return fmt.Errorf("SlotPicker: stale selection requires ErrorText")
	}
	return nil
}

func SlotPicker(p SlotPickerProps) g.Node { return SlotPickerWithSlots(p, StateSlots{}) }
func SlotPickerWithSlots(p SlotPickerProps, slots StateSlots) g.Node {
	if err := p.Validate(); err != nil {
		return invalidComponent(err)
	}
	if !p.State.ready() {
		return sharedSection(p.ComponentProps, "slot-picker", p.Label, stateBody(p.State, slots))
	}
	labels := map[string]string{"available": p.StatusLabels.Available, "booked": p.StatusLabels.Booked, "past": p.StatusLabels.Past, "unavailable": p.StatusLabels.Unavailable, "full": p.StatusLabels.Full, "stale": p.StatusLabels.Stale}
	var choices []g.Node
	for _, slot := range p.Slots {
		status, _ := SlotEligibility(slot, p.NowUTC, p.Quantity, p.Stale)
		copy := []string{slot.TimeText, p.TimeZoneLabel, labels[status]}
		if slot.Capacity == nil {
			copy = append(copy, p.StatusLabels.CapacityUnknown)
		} else if slot.CapacityText != "" {
			copy = append(copy, slot.CapacityText)
		}
		if slot.Reason != "" {
			copy = append(copy, slot.Reason)
		}
		input := []g.Node{h.Type("radio"), h.Name(p.Name), h.Value(slot.ID), g.If(p.FormID != "", g.Attr("form", p.FormID)), g.If(p.Required, h.Required()),
			g.If(p.Disabled || status != "available", h.Disabled()), g.If(slot.ID == p.SelectedID && status == "available", h.Checked())}
		if p.ErrorText != "" {
			input = append(input, g.Attr("aria-invalid", "true"), g.Attr("aria-describedby", p.ID+"-error"))
		}
		choices = append(choices, h.Label(h.Class(clChoice.Compile()), h.Input(input...), g.Text(strings.Join(copy, " · "))))
	}
	return sharedSection(p.ComponentProps, "slot-picker", p.Label, stateBody(p.State, slots), DateStrip(p.DateStrip), Text(TextProps{Content: p.SnapshotText, Size: "sm"}),
		h.FieldSet(h.Legend(h.Class(clLabel.Compile()), g.Text(p.Label)), Flex(FlexProps{Wrap: true, Gap: "2"}, choices...)),
		g.If(p.ErrorText != "", h.P(h.ID(p.ID+"-error"), h.Class(clFieldErr.Compile()), g.Text(p.ErrorText))))
}
