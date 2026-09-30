package components

import (
	"fmt"
	"math"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

type Currency string

type Money struct {
	Minor    int64    `json:"minor,string"`
	Currency Currency `json:"currency"`
}

type MoneyText struct {
	Money          Money  `json:"money"`
	Text           string `json:"text"`
	AccessibleText string `json:"accessibleText"`
}

func (m Money) Validate() error {
	if len(m.Currency) != 3 {
		return fmt.Errorf("Money: Currency must be three uppercase letters")
	}
	for _, char := range m.Currency {
		if char < 'A' || char > 'Z' {
			return fmt.Errorf("Money: invalid Currency")
		}
	}
	return nil
}
func (m MoneyText) Validate() error {
	if !required(m.Text, m.AccessibleText) {
		return fmt.Errorf("MoneyText: visible and accessible copy required")
	}
	return m.Money.Validate()
}
func moneyNode(m MoneyText) g.Node {
	return h.Span(g.Attr("aria-label", m.AccessibleText), g.Text(m.Text))
}
func addMinor(a, b int64) (int64, error) {
	if b > 0 && a > math.MaxInt64-b || b < 0 && a < math.MinInt64-b {
		return 0, fmt.Errorf("Money: addition overflow")
	}
	return a + b, nil
}

// LineTotal is exact display validation; prices and stock still come from the authority.
func LineTotal(unit Money, quantity int64) (Money, error) {
	if err := unit.Validate(); err != nil {
		return Money{}, err
	}
	if quantity <= 0 || unit.Minor < 0 || unit.Minor > math.MaxInt64/quantity {
		return Money{}, fmt.Errorf("Money: invalid quantity, negative unit price or multiplication overflow")
	}
	return Money{Minor: unit.Minor * quantity, Currency: unit.Currency}, nil
}

type SummaryLine struct {
	Key    string     `json:"key"`
	Label  string     `json:"label"`
	Amount *MoneyText `json:"amount,omitempty"`
	Text   string     `json:"text,omitempty"`
	Effect string     `json:"effect" enum:"add,included,informational"`
}

type OrderSummaryProps struct {
	ComponentProps
	Label          string        `json:"label"`
	State          ContentState  `json:"state,omitzero"`
	Lines          []SummaryLine `json:"lines,omitempty"`
	Subtotal       *MoneyText    `json:"subtotal,omitempty"`
	Total          *MoneyText    `json:"total,omitempty"`
	Complete       bool          `json:"complete,omitzero"`
	IncompleteText string        `json:"incompleteText,omitempty"`
	TotalLabel     string        `json:"totalLabel"`
	SubtotalLabel  string        `json:"subtotalLabel"`
}

type OrderSummarySlots struct {
	StateSlots
	Actions, Footnote []g.Node
}

// OrderTotal adds only explicit adjustments. Included and informational lines
// never add a second copy of an amount already in the subtotal.
func OrderTotal(subtotal Money, lines []SummaryLine) (Money, error) {
	if err := subtotal.Validate(); err != nil {
		return Money{}, err
	}
	if subtotal.Minor < 0 {
		return Money{}, fmt.Errorf("OrderSummary: negative subtotal")
	}
	total := subtotal
	for _, line := range lines {
		if line.Effect == "informational" {
			if line.Amount != nil {
				return Money{}, fmt.Errorf("OrderSummary: informational amount")
			}
			continue
		}
		if line.Effect != "add" && line.Effect != "included" {
			return Money{}, fmt.Errorf("OrderSummary: unknown effect")
		}
		if line.Amount == nil {
			return Money{}, fmt.Errorf("OrderSummary: unknown amount")
		}
		if err := line.Amount.Validate(); err != nil {
			return Money{}, err
		}
		if line.Amount.Money.Currency != subtotal.Currency {
			return Money{}, fmt.Errorf("OrderSummary: mixed currencies")
		}
		if line.Effect == "add" {
			var err error
			total.Minor, err = addMinor(total.Minor, line.Amount.Money.Minor)
			if err != nil {
				return Money{}, err
			}
		}
	}
	if total.Minor < 0 {
		return Money{}, fmt.Errorf("OrderSummary: negative total")
	}
	return total, nil
}
func (p OrderSummaryProps) Validate() error {
	if err := aggregateState(p.Label, p.State, len(p.Lines) > 0 || p.Subtotal != nil || p.Total != nil); err != nil {
		return err
	}
	if !p.State.ready() {
		return nil
	}
	if !required(p.TotalLabel, p.SubtotalLabel) {
		return fmt.Errorf("OrderSummary: localized labels required")
	}
	ids := map[string]bool{}
	var currency Currency
	for _, amount := range []*MoneyText{p.Subtotal, p.Total} {
		if amount != nil {
			if err := amount.Validate(); err != nil {
				return err
			}
			if currency != "" && currency != amount.Money.Currency {
				return fmt.Errorf("OrderSummary: mixed currencies")
			}
			currency = amount.Money.Currency
		}
	}
	for _, line := range p.Lines {
		if !required(line.Key, line.Label) || ids[line.Key] {
			return fmt.Errorf("OrderSummary: unique keys and labels required")
		}
		ids[line.Key] = true
		switch line.Effect {
		case "add", "included":
			if line.Amount == nil && !required(line.Text) {
				return fmt.Errorf("OrderSummary: unknown amount requires text")
			}
		case "informational":
			if line.Amount != nil || !required(line.Text) {
				return fmt.Errorf("OrderSummary: informational lines require text only")
			}
		default:
			return fmt.Errorf("OrderSummary: unknown effect")
		}
		if line.Amount != nil {
			if err := line.Amount.Validate(); err != nil {
				return err
			}
			if currency != "" && currency != line.Amount.Money.Currency {
				return fmt.Errorf("OrderSummary: mixed currencies")
			}
			currency = line.Amount.Money.Currency
		}
	}
	if !p.Complete {
		if p.Total != nil || !required(p.IncompleteText) {
			return fmt.Errorf("OrderSummary: incomplete requires explanation and no total")
		}
		return nil
	}
	if p.Subtotal == nil || p.Total == nil {
		return fmt.Errorf("OrderSummary: complete amounts required")
	}
	total, err := OrderTotal(p.Subtotal.Money, p.Lines)
	if err != nil {
		return err
	}
	if total != p.Total.Money {
		return fmt.Errorf("OrderSummary: supplied total does not equal subtotal plus adjustments")
	}
	return nil
}
func OrderSummary(p OrderSummaryProps) g.Node { return OrderSummaryWithSlots(p, OrderSummarySlots{}) }
func OrderSummaryWithSlots(p OrderSummaryProps, slots OrderSummarySlots) g.Node {
	if err := p.Validate(); err != nil {
		return invalidComponent(err)
	}
	if !p.State.ready() {
		if len(slots.Actions)+len(slots.Footnote) > 0 {
			return invalidComponent(fmt.Errorf("OrderSummary: absent content must clear slots"))
		}
		return sharedSection(p.ComponentProps, "order-summary", p.Label, stateBody(p.State, slots.StateSlots))
	}
	var rows []g.Node
	row := func(label string, value g.Node) {
		rows = append(rows, h.Div(h.Class(clDataHeading.Compile()), h.Dt(g.Text(label)), h.Dd(value)))
	}
	if p.Subtotal != nil {
		row(p.SubtotalLabel, moneyNode(*p.Subtotal))
	}
	for _, line := range p.Lines {
		var value g.Node = g.Text(line.Text)
		if line.Amount != nil {
			value = moneyNode(*line.Amount)
		}
		row(line.Label, value)
	}
	if p.Total != nil {
		row(p.TotalLabel, moneyNode(*p.Total))
	}
	return sharedSection(p.ComponentProps, "order-summary", p.Label, stateBody(p.State, slots.StateSlots), Heading(HeadingProps{Text: p.Label, Level: 2, Size: 4}),
		h.Dl(g.Group(rows)), g.If(!p.Complete, Text(TextProps{Content: p.IncompleteText, Size: "sm"})), g.Group(slots.Actions), g.Group(slots.Footnote))
}

type ProductCardProps struct {
	ComponentProps
	State            ContentState `json:"state,omitzero"`
	ProductID        string       `json:"productID,omitempty"`
	Title            string       `json:"title,omitempty"`
	Description      string       `json:"description,omitempty"`
	Label            string       `json:"label"`
	Href             string       `json:"href,omitempty"`
	Media            MediaProps   `json:"media,omitzero"`
	Price            *MoneyText   `json:"price,omitempty"`
	PriceText        string       `json:"priceText,omitempty"`
	Availability     string       `json:"availability,omitempty" enum:",available,sold-out,unavailable"`
	AvailabilityText string       `json:"availabilityText,omitempty"`
	Badges           []BadgeProps `json:"badges,omitempty"`
}
type ProductCardSlots struct {
	StateSlots
	Actions []g.Node
}

func availability(value string) bool {
	return value == "available" || value == "sold-out" || value == "unavailable"
}
func (p ProductCardProps) Validate() error {
	if err := aggregateState(p.Label, p.State, p.ProductID != "" || p.Title != "" || p.Description != "" || p.Href != "" || p.Media.Src != "" || p.Media.Alt != "" || p.Media.Caption != "" || p.Media.Reason != "" || p.AvailabilityText != "" || p.Price != nil || p.PriceText != "" || len(p.Badges) > 0); err != nil {
		return err
	}
	if !p.State.ready() {
		return nil
	}
	if !required(p.ProductID, p.Title, p.Href, p.AvailabilityText) || !availability(p.Availability) {
		return fmt.Errorf("ProductCard: product, link and availability required")
	}
	if p.Price == nil {
		if !required(p.PriceText) {
			return fmt.Errorf("ProductCard: unknown price needs text")
		}
	} else {
		if err := p.Price.Validate(); err != nil {
			return err
		}
		if p.Price.Money.Minor < 0 {
			return fmt.Errorf("ProductCard: negative price")
		}
	}
	return validatePhotoMedia(p.Media)
}
func ProductCard(p ProductCardProps) g.Node { return ProductCardWithSlots(p, ProductCardSlots{}) }
func ProductCardWithSlots(p ProductCardProps, slots ProductCardSlots) g.Node {
	if err := p.Validate(); err != nil {
		return invalidComponent(err)
	}
	if !p.State.ready() {
		if len(slots.Actions) > 0 {
			return invalidComponent(fmt.Errorf("ProductCard: absent content must clear actions"))
		}
		return sharedSection(p.ComponentProps, "product-card", p.Label, stateBody(p.State, slots.StateSlots))
	}
	price := g.Node(g.Text(p.PriceText))
	if p.Price != nil {
		price = moneyNode(*p.Price)
	}
	var badges []g.Node
	for _, badge := range p.Badges {
		badges = append(badges, Badge(badge))
	}
	return sharedSection(p.ComponentProps, "product-card", p.Label, stateBody(p.State, slots.StateSlots), h.A(h.Href(p.Href), Media(p.Media)),
		Heading(HeadingProps{Text: p.Title, Level: 2, Size: 4}), recoveryAction(ButtonProps{Label: p.Title, Href: p.Href, Variant: "link"}, p.Disabled),
		Text(TextProps{Content: p.Description, Size: "sm"}), price, Text(TextProps{Content: p.AvailabilityText, Size: "sm"}), g.Group(badges), g.Group(slots.Actions))
}

type BuyBarProps struct {
	ComponentProps
	Label           string       `json:"label"`
	State           ContentState `json:"state,omitzero"`
	Total           *MoneyText   `json:"total,omitempty"`
	TotalText       string       `json:"totalText,omitempty"`
	SummaryText     string       `json:"summaryText,omitempty"`
	Pending         bool         `json:"pending,omitzero"`
	UnavailableText string       `json:"unavailableText,omitempty"`
}
type BuyBarSlots struct {
	StateSlots
	PrimaryAction, SecondaryAction []g.Node
}

func (p BuyBarProps) Validate() error {
	if err := aggregateState(p.Label, p.State, p.Total != nil || p.TotalText != "" || p.SummaryText != ""); err != nil {
		return err
	}
	if !p.State.ready() {
		return nil
	}
	if !required(p.SummaryText) {
		return fmt.Errorf("BuyBar: summary required")
	}
	if p.Total != nil {
		if p.Total.Money.Minor < 0 {
			return fmt.Errorf("BuyBar: negative total")
		}
		return p.Total.Validate()
	}
	if !required(p.TotalText) {
		return fmt.Errorf("BuyBar: unknown total needs text")
	}
	return nil
}
func BuyBar(p BuyBarProps) g.Node { return BuyBarWithSlots(p, BuyBarSlots{}) }
func BuyBarWithSlots(p BuyBarProps, slots BuyBarSlots) g.Node {
	if err := p.Validate(); err != nil {
		return invalidComponent(err)
	}
	if !p.State.ready() {
		if len(slots.PrimaryAction)+len(slots.SecondaryAction) > 0 {
			return invalidComponent(fmt.Errorf("BuyBar: absent content must clear actions"))
		}
		return sharedSection(p.ComponentProps, "buy-bar", p.Label, stateBody(p.State, slots.StateSlots))
	}
	amount := g.Node(g.Text(p.TotalText))
	if p.Total != nil {
		amount = moneyNode(*p.Total)
	}
	var primary g.Node
	if !p.Pending && !p.Disabled && p.UnavailableText == "" {
		primary = g.Group(slots.PrimaryAction)
	}
	return sharedSection(p.ComponentProps, "buy-bar", p.Label, stateBody(p.State, slots.StateSlots),
		Flex(FlexProps{Wrap: true, Gap: "4", Align: "center"}, Stack(StackProps{Gap: "2"}, amount, Text(TextProps{Content: p.SummaryText, Size: "sm"}), Text(TextProps{Content: p.UnavailableText, Size: "sm"})), primary, g.Group(slots.SecondaryAction)))
}

type CartLine struct {
	ID               string             `json:"id"`
	ProductID        string             `json:"productID"`
	VariantID        string             `json:"variantID,omitempty"`
	Title            string             `json:"title"`
	Description      string             `json:"description,omitempty"`
	Media            *MediaProps        `json:"media,omitempty"`
	UnitPrice        MoneyText          `json:"unitPrice"`
	Quantity         int64              `json:"quantity,string"`
	QuantityInput    QuantityInputProps `json:"quantityInput"`
	Availability     string             `json:"availability"`
	AvailabilityText string             `json:"availabilityText"`
	LineTotal        MoneyText          `json:"lineTotal"`
	Revision         string             `json:"revision"`
}
type CartProps struct {
	ComponentProps
	Label      string            `json:"label"`
	State      ContentState      `json:"state,omitzero"`
	Lines      []CartLine        `json:"lines,omitempty"`
	Summary    OrderSummaryProps `json:"summary,omitzero"`
	QuoteState string            `json:"quoteState,omitempty" enum:",current,stale,incomplete"`
	QuoteText  string            `json:"quoteText,omitempty"`
	ResultKey  string            `json:"resultKey,omitempty"`
	Pending    bool              `json:"pending,omitzero"`
}
type CartSlots struct {
	StateSlots
	LineActions func(CartLine) g.Node
	Checkout    []g.Node
}

func (p CartProps) Validate() error {
	if err := aggregateState(p.Label, p.State, len(p.Lines) > 0 || p.Summary.Subtotal != nil || p.Summary.Total != nil || len(p.Summary.Lines) > 0 || p.ResultKey != ""); err != nil {
		return err
	}
	if !p.State.ready() {
		return nil
	}
	if !required(p.ResultKey, p.QuoteText) {
		return fmt.Errorf("Cart: snapshot identity and quote explanation required")
	}
	if p.QuoteState != "current" && p.QuoteState != "stale" && p.QuoteState != "incomplete" {
		return fmt.Errorf("Cart: unknown quote state")
	}
	if err := p.Summary.Validate(); err != nil {
		return err
	}
	ids := map[string]bool{}
	var subtotal Money
	for _, line := range p.Lines {
		if !required(line.ID, line.ProductID, line.Title, line.Revision, line.AvailabilityText) || ids[line.ID] || !availability(line.Availability) {
			return fmt.Errorf("Cart: unique line identity, revision and availability required")
		}
		ids[line.ID] = true
		if err := line.UnitPrice.Validate(); err != nil {
			return err
		}
		if err := line.LineTotal.Validate(); err != nil {
			return err
		}
		total, err := LineTotal(line.UnitPrice.Money, line.Quantity)
		if err != nil {
			return err
		}
		if total != line.LineTotal.Money {
			return fmt.Errorf("Cart: line total mismatch")
		}
		if subtotal.Currency != "" && subtotal.Currency != total.Currency {
			return fmt.Errorf("Cart: mixed currencies")
		}
		subtotal.Currency = total.Currency
		subtotal.Minor, err = addMinor(subtotal.Minor, total.Minor)
		if err != nil {
			return err
		}
		if line.QuantityInput.Value != line.Quantity {
			return fmt.Errorf("Cart: quantity control differs from line")
		}
		quantity := line.QuantityInput
		quantity.ID = p.ID + "-quantity-" + line.ID
		if err := quantity.Validate(); err != nil {
			return err
		}
		if line.Media != nil {
			if err := validatePhotoMedia(*line.Media); err != nil {
				return err
			}
		}
	}
	if p.Summary.Subtotal != nil && (p.Summary.Subtotal.Money.Minor != subtotal.Minor || len(p.Lines) > 0 && p.Summary.Subtotal.Money.Currency != subtotal.Currency) {
		return fmt.Errorf("Cart: subtotal differs from lines")
	}
	if p.QuoteState == "current" && !p.Summary.Complete {
		return fmt.Errorf("Cart: current quote requires complete summary")
	}
	return nil
}
func (p CartProps) CanCheckout() bool {
	if p.Validate() != nil || !p.State.ready() || p.Disabled || p.Pending || len(p.Lines) == 0 || p.QuoteState != "current" || !p.Summary.Complete {
		return false
	}
	for _, line := range p.Lines {
		if line.Availability != "available" {
			return false
		}
	}
	return true
}
func Cart(p CartProps) g.Node { return CartWithSlots(p, CartSlots{}) }
func CartWithSlots(p CartProps, slots CartSlots) g.Node {
	if err := p.Validate(); err != nil {
		return invalidComponent(err)
	}
	if !p.State.ready() {
		if len(slots.Checkout) > 0 {
			return invalidComponent(fmt.Errorf("Cart: absent content must clear checkout"))
		}
		return sharedSection(p.ComponentProps, "cart", p.Label, stateBody(p.State, slots.StateSlots))
	}
	var rows []g.Node
	for _, line := range p.Lines {
		quantity := line.QuantityInput
		quantity.ID = p.ID + "-quantity-" + line.ID
		quantity.Pending = quantity.Pending || p.Pending
		quantity.Disabled = quantity.Disabled || p.Disabled || line.Availability != "available"
		var media, action g.Node
		if line.Media != nil {
			media = Media(*line.Media)
		}
		if slots.LineActions != nil {
			action = slots.LineActions(line)
		}
		rows = append(rows, h.Li(h.Class(clDataList.Compile()), g.Attr("data-cart-line", line.ID), media, Heading(HeadingProps{Text: line.Title, Level: 3, Size: 5}), Text(TextProps{Content: line.Description, Size: "sm"}), moneyNode(line.UnitPrice), Text(TextProps{Content: line.AvailabilityText, Size: "sm"}), QuantityInput(quantity), moneyNode(line.LineTotal), action))
	}
	var checkout g.Node
	if p.CanCheckout() {
		checkout = g.Group(slots.Checkout)
	}
	return sharedSection(p.ComponentProps, "cart", p.Label, stateBody(p.State, slots.StateSlots), h.Ul(h.Class(clDataList.Compile()), g.Group(rows)), Text(TextProps{Content: p.QuoteText, Size: "sm"}), OrderSummary(p.Summary), checkout)
}
