package components

import (
	"fmt"
	"io"
	"strings"
	"time"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

type TimelineSlots struct{ EmptyAction, RetryAction []g.Node }

func (p TimeText) Validate() error {
	if p.AtUTC.IsZero() || p.AtUTC.Location() != time.UTC || p.AtUTC.Year() < 1 || p.AtUTC.Year() > 9999 || strings.TrimSpace(p.Text) == "" {
		return fmt.Errorf("TimeText: a nonzero UTC instant and localized Text are required")
	}
	return nil
}

func (p TimelineProps) Validate() error {
	for _, label := range []string{p.Label, p.ActorLabel, p.TimeLabel, p.DetailsLabel} {
		if strings.TrimSpace(label) == "" {
			return fmt.Errorf("Timeline: localized labels are required")
		}
	}
	if err := p.State.Validate(); err != nil {
		return err
	}
	if p.Layout != "" && p.Layout != "timeline" && p.Layout != "audit" {
		return fmt.Errorf("Timeline: unknown Layout")
	}
	if !p.State.ready() {
		if len(p.Items) != 0 || p.More != nil {
			return fmt.Errorf("Timeline: absent states must clear items and continuation")
		}
		return nil
	}
	if len(p.Items) == 0 {
		return fmt.Errorf("Timeline: ready requires items; use empty for no events")
	}
	ids := map[string]bool{}
	for _, item := range p.Items {
		if strings.TrimSpace(item.ID) == "" || ids[item.ID] || strings.TrimSpace(item.ActorText) == "" || strings.TrimSpace(item.Summary) == "" {
			return fmt.Errorf("Timeline: unique IDs, ActorText and Summary are required")
		}
		ids[item.ID] = true
		if err := item.Time.Validate(); err != nil {
			return err
		}
		for _, change := range item.Changes {
			if strings.TrimSpace(change.Label) == "" {
				return fmt.Errorf("Timeline: change Label is required")
			}
			if change.Redacted {
				if change.BeforeText != "" || change.AfterText != "" || strings.TrimSpace(change.RedactedText) == "" {
					return fmt.Errorf("Timeline: redacted changes must contain no before/after values")
				}
			} else if strings.TrimSpace(p.BeforeLabel) == "" || strings.TrimSpace(p.AfterLabel) == "" || strings.TrimSpace(change.BeforeText) == "" || strings.TrimSpace(change.AfterText) == "" {
				return fmt.Errorf("Timeline: changes require localized before/after labels and values")
			}
		}
	}
	if p.More != nil {
		return validateChoices([]ChoiceLink{*p.More})
	}
	return nil
}

func Timeline(p TimelineProps) g.Node { return TimelineWithSlots(p, TimelineSlots{}) }

func TimelineWithSlots(p TimelineProps, slots TimelineSlots) g.Node {
	if err := p.Validate(); err != nil {
		return g.NodeFunc(func(io.Writer) error { return err })
	}
	root := append(baseAttrs(p.ComponentProps), classes(clDataList.Compile(), p.Class), g.Attr("data-component", "timeline"), g.Attr("aria-label", p.Label))
	root = append(root, contentStateNode(p.State, slots.EmptyAction, slots.RetryAction, TableSkeleton(TableSkeletonProps{Columns: 3})))
	if !p.State.ready() {
		return h.Section(root...)
	}
	if p.Layout == "audit" {
		rows := make([]TableRow, 0, len(p.Items))
		items := map[string]TimelineItem{}
		for _, item := range p.Items {
			rows = append(rows, TableRow{ID: item.ID})
			items[item.ID] = item
		}
		root = append(root, TableWithSlots(TableProps{Label: p.Label, Columns: []TableColumn{{Key: "actor", Label: p.ActorLabel}, {Key: "time", Label: p.TimeLabel}, {Key: "details", Label: p.DetailsLabel}}, Rows: rows}, TableSlots{
			Cell: func(row TableRow, col TableColumn) g.Node {
				item := items[row.ID]
				switch col.Key {
				case "actor":
					return timelineActor(item)
				case "time":
					return timelineTime(item.Time)
				default:
					return timelineContent(p, item)
				}
			},
		}))
	} else {
		var rows []g.Node
		for _, item := range p.Items {
			rows = append(rows, h.Li(h.Class(clDataList.Compile()), g.Attr("data-event-id", item.ID),
				Flex(FlexProps{Wrap: true, Gap: "3", Align: "center"}, timelineActor(item), timelineTime(item.Time)), timelineContent(p, item)))
		}
		root = append(root, h.Ol(h.Class(clDataList.Compile()), g.Group(rows)))
	}
	if p.More != nil {
		root = append(root, choiceLinks([]ChoiceLink{*p.More}, p.Disabled))
	}
	return h.Section(root...)
}

func timelineActor(item TimelineItem) g.Node {
	if item.ActorAvatar == nil {
		return Text(TextProps{Content: item.ActorText, Size: "sm", Element: "span"})
	}
	avatar := *item.ActorAvatar
	// Actor copy is said once; the optional portrait does not invent navigation.
	avatar.Decorative, avatar.Label, avatar.Href = true, "", ""
	return Flex(FlexProps{Gap: "2", Align: "center"}, Avatar(avatar), Text(TextProps{Content: item.ActorText, Size: "sm", Element: "span"}))
}

func timelineTime(value TimeText) g.Node {
	return Time(TimeProps{ComponentProps: ComponentProps{Class: clDataCount.Compile()}, Instant: value})
}

func timelineContent(p TimelineProps, item TimelineItem) g.Node {
	var nodes []g.Node
	nodes = append(nodes, Text(TextProps{Content: item.Summary, Size: "sm"}))
	if item.StatusLabel != "" {
		nodes = append(nodes, Badge(BadgeProps{Label: item.StatusLabel, Tone: item.Tone}))
	}
	if len(item.Changes) != 0 {
		var changes []g.Node
		for _, change := range item.Changes {
			values := []DetailItem{{Label: p.BeforeLabel, Value: change.BeforeText}, {Label: p.AfterLabel, Value: change.AfterText}}
			if change.Redacted {
				values = []DetailItem{{Label: change.Label, Value: change.RedactedText}}
			}
			changes = append(changes, DetailList(DetailListProps{Title: change.Label, Items: values}))
		}
		nodes = append(nodes, h.Details(h.Summary(h.Class(clDataDisclosure.Compile()), g.Text(p.DetailsLabel)), Stack(StackProps{Gap: "4"}, changes...)))
	}
	if item.DetailsHref != "" {
		nodes = append(nodes, recoveryAction(ButtonProps{Label: p.DetailsLabel, Href: item.DetailsHref, Variant: "link"}, p.Disabled))
	}
	return Stack(StackProps{Gap: "2"}, nodes...)
}
