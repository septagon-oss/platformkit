package examples

import (
	"fmt"
	"math"

	g "maragu.dev/gomponents"

	c "github.com/septagon-oss/platformkit/ui/components"
)

func productCardExamplesForLocale(w sharedCopy) []Example {
	var entries []Example
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

	return entries
}

func optionChipsExamplesForLocale(w sharedCopy) []Example {
	var entries []Example
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

	return entries
}

func quantityExamplesForLocale(w sharedCopy) []Example {
	var entries []Example
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

	return entries
}

func buyBarExamplesForLocale(w sharedCopy) []Example {
	var entries []Example
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

	return entries
}

func orderSummaryExamplesForLocale(w sharedCopy) []Example {
	var entries []Example
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

	return entries
}

func cartExamplesForLocale(w sharedCopy) []Example {
	var entries []Example
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
	return entries
}
