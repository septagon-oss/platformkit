package components

import (
	"cmp"
	"fmt"
	"io"
	"strings"

	g "maragu.dev/gomponents"
)

// NoticeSlots reuses Alert's trusted icon and native form/action seams.
type NoticeSlots = AlertSlots

// Notice renders a persistent, caller-localized notice. It never auto-retries.
func Notice(p NoticeProps) g.Node { return NoticeWithSlots(p, NoticeSlots{}) }

// Validate reports malformed composition without turning it into visible UI copy.
func (p NoticeProps) Validate() error {
	if strings.TrimSpace(p.Text) == "" {
		return fmt.Errorf("Notice: Text is required")
	}
	switch p.Tone {
	case "", "danger", "warning", "info", "ok":
	default:
		return fmt.Errorf("Notice: unknown Tone %q", p.Tone)
	}
	if err := validateLive(p.Live); err != nil {
		return err
	}
	if p.Dismissible && strings.TrimSpace(p.DismissLabel) == "" {
		return fmt.Errorf("Notice: DismissLabel is required when dismissible")
	}
	return validateRecoveryAction(p.Action)
}

// NoticeWithSlots shares Alert's styling, dismissal and announcement implementation.
func NoticeWithSlots(p NoticeProps, slots NoticeSlots) g.Node {
	if err := p.Validate(); err != nil {
		return g.NodeFunc(func(io.Writer) error { return err })
	}
	if p.Action != nil && len(slots.Actions) != 0 {
		return g.NodeFunc(func(io.Writer) error { return fmt.Errorf("Notice: Action and Actions slot are exclusive") })
	}
	if p.Action != nil {
		slots.Actions = []g.Node{recoveryAction(*p.Action, p.Disabled)}
	}
	tone := cmp.Or(p.Tone, "danger")
	if tone == "ok" {
		tone = "success"
	}
	return alertWithSlots(AlertProps{
		ComponentProps: p.ComponentProps, Title: p.Title, Message: p.Text,
		Tone: tone, Live: cmp.Or(p.Live, "polite"),
		Dismissible: p.Dismissible, DismissLabel: p.DismissLabel,
	}, slots, "text")
}

func validateLive(live string) error {
	switch live {
	case "", "polite", "assertive", "off":
		return nil
	default:
		return fmt.Errorf("notice: unknown Live %q", live)
	}
}

func validateRecoveryAction(action *ButtonProps) error {
	if action != nil && strings.TrimSpace(cmp.Or(action.Label, action.AriaLabel)) == "" {
		return fmt.Errorf("recovery action: Label or AriaLabel is required")
	}
	return nil
}

func recoveryAction(p ButtonProps, disabled bool) g.Node {
	p.Disabled = p.Disabled || disabled || p.Loading
	p.Class = strings.TrimSpace(p.Class + " " + clRecoveryAction.Compile())
	return Button(p)
}
