package resource

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/septagon-oss/platformkit/kit/entity/display"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/ui/components"
	g "maragu.dev/gomponents"
)

type reading struct {
	text, tone string
	err        error
	node       g.Node
}

func invalid(err error) g.Node { return g.NodeFunc(func(io.Writer) error { return err }) }

func value(f present.ResolvedField, row map[string]any, o Options) reading {
	raw := row[f.Name]
	text := scalarText(raw)
	out := reading{text: text, tone: "neutral"}
	if raw == nil || text == "" {
		out.text = f.Reading.Missing
		return out
	}
	switch {
	case f.Reading.Money != nil:
		var err error
		out.text, err = money(raw, display.Text(row[f.Reading.Money.CurrencyField]), f.Reading.Money.Scale)
		if err != nil {
			out.err = fmt.Errorf("resource: %s: %w", f.Name, err)
			out.node = invalid(out.err)
		}
	case f.Kind == "boolean":
		b, ok := raw.(bool)
		if !ok {
			out.err = fmt.Errorf("resource: %s requires a boolean", f.Name)
			out.node = invalid(out.err)
			break
		}
		out.text = f.Reading.False
		if b {
			out.text = f.Reading.True
		}
	case f.Reading.Time != nil:
		if at, ok := display.Instant(text); ok {
			return timeValue(f, at, o)
		}
	case f.Reading.Precision != nil:
		n, err := exactInteger(raw)
		if err != nil {
			out.err = fmt.Errorf("resource: %s: %w", f.Name, err)
			out.node = invalid(out.err)
		} else {
			out.text = strconv.FormatInt(n, 10)
		}
	}
	for _, option := range f.Options {
		if option.Value == text {
			out.text, out.tone = option.Label, option.Tone
			break
		}
	}
	if f.Kind == "enum" {
		out.node = components.Badge(components.BadgeProps{Label: out.text, Tone: out.tone})
	}
	return out
}

func exactInteger(raw any) (int64, error) {
	if f, ok := raw.(float64); ok {
		if math.IsNaN(f) || math.IsInf(f, 0) || math.Trunc(f) != f || math.Abs(f) > 9007199254740991 {
			return 0, fmt.Errorf("unsafe integer %v", f)
		}
	}
	n, err := strconv.ParseInt(scalarText(raw), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("expected an exact int64: %w", err)
	}
	return n, nil
}

func money(raw any, currency string, scale int) (string, error) {
	n, err := exactInteger(raw)
	if err != nil {
		return "", err
	}
	if err := (components.Money{Minor: n, Currency: components.Currency(currency)}).Validate(); err != nil {
		return "", err
	}
	if scale < 0 || scale > 3 {
		return "", fmt.Errorf("money scale must be 0..3")
	}
	digits := strconv.FormatInt(n, 10)
	sign := ""
	if strings.HasPrefix(digits, "-") {
		sign, digits = "-", digits[1:]
	}
	for len(digits) <= scale {
		digits = "0" + digits
	}
	if scale > 0 {
		digits = digits[:len(digits)-scale] + "." + digits[len(digits)-scale:]
	}
	return currency + " " + sign + digits, nil
}

func timeValue(f present.ResolvedField, at time.Time, o Options) reading {
	location := o.Location
	if location == nil {
		location = time.UTC
	}
	local := at.In(location)
	text := local.Format("2006-01-02 15:04")
	rule := f.Reading.Time
	attrs := map[string]string{"data-time-policy": "1", "data-time-kind": rule.Kind, "data-time-relative": strconv.FormatBool(rule.Relative), "data-time-now": o.Text("screens.now", "now")}
	if o.Locale != nil {
		attrs["data-time-language"] = o.Locale.Language
	}
	if o.Location != nil {
		attrs["data-time-zone"] = location.String()
		_, offset := local.Zone()
		attrs["data-time-offset"] = strconv.Itoa(offset)
	}
	if rule.Kind == "date" {
		text = local.Format("2006-01-02")
	} else if rule.Relative && !o.Rendered.IsZero() {
		unit, n := present.Ago(at, o.Rendered)
		attrs["data-time-unit"], attrs["data-time-count"] = unit, strconv.Itoa(n)
		text = relativeWords(o, unit, n)
	}
	return reading{text: text, tone: "neutral", node: components.Time(components.TimeProps{
		ComponentProps: components.ComponentProps{Attrs: attrs},
		// The title is the moment with the zone it is stated in, at the second a
		// person can read; the sub-second instant a machine reads is `datetime`.
		// Fractional digits in a title are digits nobody compares, and the title
		// is what stands when no browser says the moment again.
		Instant: components.TimeText{AtUTC: at, Text: text}, Title: local.Format(time.RFC3339),
	})}
}

type relativeCopy struct{ key, english string }

var relativeCopyByUnit = map[string][4]relativeCopy{
	"second": {{"screens.relative.second_past", "%d second ago"}, {"screens.relative.seconds_past", "%d seconds ago"}, {"screens.relative.second_future", "in %d second"}, {"screens.relative.seconds_future", "in %d seconds"}},
	"minute": {{"screens.relative.minute_past", "%d minute ago"}, {"screens.relative.minutes_past", "%d minutes ago"}, {"screens.relative.minute_future", "in %d minute"}, {"screens.relative.minutes_future", "in %d minutes"}},
	"hour":   {{"screens.relative.hour_past", "%d hour ago"}, {"screens.relative.hours_past", "%d hours ago"}, {"screens.relative.hour_future", "in %d hour"}, {"screens.relative.hours_future", "in %d hours"}},
	"day":    {{"screens.relative.day_past", "%d day ago"}, {"screens.relative.days_past", "%d days ago"}, {"screens.relative.day_future", "in %d day"}, {"screens.relative.days_future", "in %d days"}},
}

func relativeWords(o Options, unit string, n int) string {
	if n == 0 {
		return o.Text("screens.now", "now")
	}
	count, index := n, 0
	if count < 0 {
		count = -count
	} else {
		index = 2
	}
	if count != 1 {
		index++
	}
	copy := relativeCopyByUnit[unit][index]
	return o.Text(copy.key, copy.english, count)
}

// Value renders a resolved field against its row, including sibling currency and
// the explicit environment. Invalid numeric inputs return an HTML render error.
func Value(f present.ResolvedField, row map[string]any, o Options) g.Node {
	read := value(f, row, o)
	if read.node != nil {
		return read.node
	}
	return g.Text(read.text)
}

func scalarText(raw any) string {
	switch n := raw.(type) {
	case int64:
		return strconv.FormatInt(n, 10)
	case int:
		return strconv.Itoa(n)
	case time.Time:
		return n.Format(time.RFC3339Nano)
	}
	return display.Text(raw)
}
