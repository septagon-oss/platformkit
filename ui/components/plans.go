package components

import (
	"fmt"
	"slices"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

type PlanFeature struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	State string `json:"state" enum:"included,excluded,value,unknown"`
	Text  string `json:"text"`
}
type Plan struct {
	ID              string        `json:"id"`
	Name            string        `json:"name"`
	Description     string        `json:"description,omitempty"`
	Price           *MoneyText    `json:"price,omitempty"`
	PriceText       string        `json:"priceText,omitempty"`
	PeriodText      string        `json:"periodText"`
	BadgeText       string        `json:"badgeText,omitempty"`
	Recommended     bool          `json:"recommended,omitzero"`
	Features        []PlanFeature `json:"features"`
	Action          ButtonProps   `json:"action"`
	Unavailable     bool          `json:"unavailable,omitzero"`
	UnavailableText string        `json:"unavailableText,omitempty"`
}
type PricingTiersProps struct {
	ComponentProps
	Label           string       `json:"label"`
	State           ContentState `json:"state,omitzero"`
	Plans           []Plan       `json:"plans,omitempty"`
	Periods         []ChoiceLink `json:"periods,omitempty"`
	SelectedPeriod  string       `json:"selectedPeriod,omitempty"`
	CurrentPlanID   string       `json:"currentPlanID,omitempty"`
	CurrentPlanText string       `json:"currentPlanText,omitempty"`
}
type FeatureHeading struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}
type PlanComparisonProps struct {
	ComponentProps
	Label           string           `json:"label"`
	State           ContentState     `json:"state,omitzero"`
	Plans           []Plan           `json:"plans,omitempty"`
	Features        []FeatureHeading `json:"features,omitempty"`
	CurrentPlanID   string           `json:"currentPlanID,omitempty"`
	CurrentPlanText string           `json:"currentPlanText,omitempty"`
	Caption         string           `json:"caption"`
}

func validatePlans(plans []Plan, currentID, currentText string) error {
	if len(plans) == 0 {
		return fmt.Errorf("Plans: ready requires plans")
	}
	if currentID != "" && !required(currentText) {
		return fmt.Errorf("Plans: current plan needs localized text")
	}
	ids := map[string]bool{}
	for _, plan := range plans {
		if !required(plan.ID, plan.Name, plan.PeriodText, plan.Action.Label) || ids[plan.ID] {
			return fmt.Errorf("Plans: unique identities and labels required")
		}
		ids[plan.ID] = true
		if plan.Recommended && !required(plan.BadgeText) || plan.Unavailable && !required(plan.UnavailableText) {
			return fmt.Errorf("Plans: recommendation and unavailability need text")
		}
		if plan.Price == nil {
			if !required(plan.PriceText) {
				return fmt.Errorf("Plans: unknown price needs text")
			}
		} else {
			if err := plan.Price.Validate(); err != nil {
				return err
			}
			if plan.Price.Money.Minor < 0 {
				return fmt.Errorf("Plans: negative price")
			}
		}
		features := map[string]bool{}
		for _, feature := range plan.Features {
			if !required(feature.Key, feature.Label, feature.Text) || features[feature.Key] {
				return fmt.Errorf("Plans: unique labeled features required")
			}
			features[feature.Key] = true
			switch feature.State {
			case "included", "excluded", "value", "unknown":
			default:
				return fmt.Errorf("Plans: unknown feature state")
			}
		}
	}
	if currentID != "" && !ids[currentID] {
		return fmt.Errorf("Plans: current plan is absent")
	}
	return nil
}
func (p PricingTiersProps) Validate() error {
	if err := aggregateState(p.Label, p.State, len(p.Plans) > 0 || p.CurrentPlanID != ""); err != nil {
		return err
	}
	if !p.State.ready() {
		return nil
	}
	if err := validatePlans(p.Plans, p.CurrentPlanID, p.CurrentPlanText); err != nil {
		return err
	}
	if err := validateChoices(p.Periods); err != nil {
		return err
	}
	if len(p.Periods) > 0 {
		if slices.IndexFunc(p.Periods, func(c ChoiceLink) bool { return c.Key == p.SelectedPeriod && !c.Disabled }) < 0 {
			return fmt.Errorf("Plans: selected period must be enabled")
		}
	}
	return nil
}
func PricingTiers(p PricingTiersProps) g.Node { return PricingTiersWithSlots(p, StateSlots{}) }
func PricingTiersWithSlots(p PricingTiersProps, slots StateSlots) g.Node {
	if err := p.Validate(); err != nil {
		return invalidComponent(err)
	}
	if !p.State.ready() {
		return sharedSection(p.ComponentProps, "pricing-tiers", p.Label, stateBody(p.State, slots))
	}
	periods := slices.Clone(p.Periods)
	for i := range periods {
		periods[i].Selected = periods[i].Key == p.SelectedPeriod
	}
	var plans []g.Node
	for _, plan := range p.Plans {
		plans = append(plans, planCard(plan, p.CurrentPlanID, p.CurrentPlanText, p.Disabled))
	}
	return sharedSection(p.ComponentProps, "pricing-tiers", p.Label, stateBody(p.State, slots), choiceLinks(periods, p.Disabled), Grid(GridProps{Columns: "1", MD: "2", Gap: "6"}, plans...))
}
func planCard(plan Plan, currentID, currentText string, disabled bool) g.Node {
	var body []g.Node
	if plan.Price != nil {
		body = append(body, moneyNode(*plan.Price))
	} else {
		body = append(body, g.Text(plan.PriceText))
	}
	body = append(body, Text(TextProps{Content: plan.PeriodText, Size: "sm"}))
	if plan.ID == currentID {
		body = append(body, Badge(BadgeProps{Label: currentText, Tone: "info"}))
	}
	if plan.BadgeText != "" {
		body = append(body, Badge(BadgeProps{Label: plan.BadgeText, Tone: "neutral"}))
	}
	var features []g.Node
	for _, feature := range plan.Features {
		features = append(features, h.Li(g.Text(feature.Label+" · "+feature.Text)))
	}
	body = append(body, h.Ul(g.Group(features)), Text(TextProps{Content: plan.UnavailableText, Size: "sm"}))
	action := plan.Action
	action.Disabled = action.Disabled || disabled || plan.Unavailable || plan.ID == currentID
	if action.Disabled || action.Variant == "" && !plan.Recommended {
		action.Variant = "outline"
	}
	return CardWithSlots(CardProps{Title: plan.Name, Description: plan.Description}, CardSlots{Content: []g.Node{Stack(StackProps{Gap: "3"}, body...)}, Footer: []g.Node{recoveryAction(action, false)}})
}
func (p PlanComparisonProps) Validate() error {
	if err := aggregateState(p.Label, p.State, len(p.Plans) > 0 || len(p.Features) > 0 || p.CurrentPlanID != ""); err != nil {
		return err
	}
	if !p.State.ready() {
		return nil
	}
	if !required(p.Caption) || len(p.Features) == 0 {
		return fmt.Errorf("PlanComparison: caption and feature headings required")
	}
	if err := validatePlans(p.Plans, p.CurrentPlanID, p.CurrentPlanText); err != nil {
		return err
	}
	keys := map[string]bool{}
	for _, f := range p.Features {
		if !required(f.Key, f.Label) || keys[f.Key] {
			return fmt.Errorf("PlanComparison: unique feature headings required")
		}
		keys[f.Key] = true
	}
	for _, plan := range p.Plans {
		if len(plan.Features) != len(keys) {
			return fmt.Errorf("PlanComparison: each plan must supply every cell")
		}
		for _, f := range plan.Features {
			if !keys[f.Key] {
				return fmt.Errorf("PlanComparison: unknown feature cell")
			}
		}
	}
	return nil
}
func PlanComparison(p PlanComparisonProps) g.Node { return PlanComparisonWithSlots(p, StateSlots{}) }
func PlanComparisonWithSlots(p PlanComparisonProps, slots StateSlots) g.Node {
	if err := p.Validate(); err != nil {
		return invalidComponent(err)
	}
	if !p.State.ready() {
		return sharedSection(p.ComponentProps, "plan-comparison", p.Label, stateBody(p.State, slots))
	}
	columns := []TableColumn{{Key: "feature", Label: p.Caption, Primary: true, RowHeader: true}}
	for _, plan := range p.Plans {
		label := plan.Name
		if plan.ID == p.CurrentPlanID {
			label += " · " + p.CurrentPlanText
		}
		if plan.Recommended {
			label += " · " + plan.BadgeText
		}
		columns = append(columns, TableColumn{Key: plan.ID, Label: label})
	}
	var rows []TableRow
	for _, heading := range p.Features {
		label := heading.Label
		if heading.Description != "" {
			label += " · " + heading.Description
		}
		cells := map[string]any{"feature": label}
		for _, plan := range p.Plans {
			for _, feature := range plan.Features {
				if feature.Key == heading.Key {
					cells[plan.ID] = feature.Text
				}
			}
		}
		rows = append(rows, TableRow{ID: heading.Key, Cells: cells})
	}
	var details []g.Node
	for _, plan := range p.Plans {
		details = append(details, h.Details(h.Summary(h.Class(clDataDisclosure.Compile()), g.Text(plan.Name)), planCard(plan, p.CurrentPlanID, p.CurrentPlanText, p.Disabled)))
	}
	return sharedSection(p.ComponentProps, "plan-comparison", p.Label, stateBody(p.State, slots), Table(TableProps{Label: p.Caption, Columns: columns, Rows: rows}), g.Group(details))
}
