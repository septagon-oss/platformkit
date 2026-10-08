package components

import (
	"fmt"
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

func (p ContentState) ready() bool { return p.Status == "" || p.Status == MediaReady }

// Validate checks the visible state, not the authority of the values supplied to it.
func (p ContentState) Validate() error {
	switch p.Status {
	case "", MediaReady:
	case MediaLoading:
		if strings.TrimSpace(p.LoadingLabel) == "" {
			return fmt.Errorf("ContentState: LoadingLabel is required")
		}
	case MediaEmpty, MediaFailed, MediaRefused:
		if strings.TrimSpace(p.Title) == "" || strings.TrimSpace(p.Text) == "" {
			return fmt.Errorf("ContentState: Title and Text are required for absent content")
		}
	default:
		return fmt.Errorf("ContentState: unknown Status %q", p.Status)
	}
	if p.Offline && ((!p.ready() && p.Status != MediaFailed) || strings.TrimSpace(p.OfflineText) == "") {
		return fmt.Errorf("ContentState: Offline requires ready/failed and OfflineText")
	}
	return nil
}

func contentStateNode(p ContentState, empty, retry []g.Node, loading g.Node) g.Node {
	var nodes []g.Node
	if p.Offline {
		nodes = append(nodes, Notice(NoticeProps{Text: p.OfflineText, Tone: "warning", Live: "off"}))
	}
	switch p.Status {
	case MediaLoading:
		nodes = append(nodes, h.Div(h.Role("status"), g.Attr("aria-busy", "true"), g.Attr("aria-live", "polite"),
			h.P(h.Class(clDataCount.Compile()), g.Text(p.LoadingLabel)), loading))
	case MediaEmpty:
		nodes = append(nodes, EmptyStateWithSlots(EmptyStateProps{Title: p.Title, Text: p.Text}, EmptyStateSlots{Actions: empty}))
	case MediaFailed:
		nodes = append(nodes, NoticeWithSlots(NoticeProps{Title: p.Title, Text: p.Text}, NoticeSlots{Actions: retry}))
	case MediaRefused:
		nodes = append(nodes, Notice(NoticeProps{Title: p.Title, Text: p.Text, Tone: "info"}))
	}
	return g.Group(nodes)
}

func validateChoices(choices []ChoiceLink) error {
	keys := map[string]bool{}
	for _, choice := range choices {
		if strings.TrimSpace(choice.Key) == "" || keys[choice.Key] || strings.TrimSpace(choice.Label) == "" {
			return fmt.Errorf("ChoiceLink: unique Key and Label are required")
		}
		keys[choice.Key] = true
		if !choice.Disabled && strings.TrimSpace(choice.Href) == "" {
			return fmt.Errorf("ChoiceLink: enabled choice requires Href")
		}
		if (choice.RemoveHref == "") != (strings.TrimSpace(choice.RemoveLabel) == "") {
			return fmt.Errorf("ChoiceLink: RemoveHref and RemoveLabel are required together")
		}
	}
	return nil
}

func choiceLinks(choices []ChoiceLink, disabled bool) g.Node {
	if len(choices) == 0 {
		return nil
	}
	var nodes []g.Node
	for _, choice := range choices {
		variant := "ghost"
		attrs := map[string]string{}
		if choice.Selected {
			variant = "outline"
			attrs["aria-current"] = "true"
		}
		button := ButtonProps{ComponentProps: ComponentProps{Disabled: disabled || choice.Disabled, Attrs: attrs},
			Label: choice.Label, Href: choice.Href, Variant: variant}
		if button.Disabled && button.Href == "" {
			// A disabled choice has no synthesized destination.
			button.Type = "button"
		}
		nodes = append(nodes, recoveryAction(button, false))
		if choice.RemoveHref != "" {
			nodes = append(nodes, recoveryAction(ButtonProps{Label: choice.RemoveLabel, Href: choice.RemoveHref, Variant: "ghost"}, disabled || choice.Disabled))
		}
	}
	return Flex(FlexProps{Gap: "2", Wrap: true}, nodes...)
}
