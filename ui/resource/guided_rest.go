package resource

import (
	"strconv"
	"time"

	c "github.com/septagon-oss/platformkit/ui/components"
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

func (p GuidedPage) restBody(o Options) []g.Node {
	declared, _ := p.Rest()
	seconds := declared + int32(p.Extra)
	deadline := ""
	if progress := p.Session.Progress; progress != nil {
		at := progress.Completed[p.position()].CompletedAt.Add(time.Duration(seconds) * time.Second)
		deadline = strconv.FormatInt(at.UnixMilli(), 10)
	}
	nextTitle := p.word("finish")
	var picture g.Node
	if step, _, ok := p.Session.Pair(p.position() + 1); ok {
		next := p.Session.Steps[step]
		nextTitle, picture = next.Title, guidedPicture(next.Picture, next.Title, true, o)
	}
	add := p.restURL() + "?extra=" + strconv.Itoa(min(p.Extra+15, 86400-int(declared)))
	countdown := c.Stack(c.StackProps{Gap: "6"},
		h.Div(g.Attr("data-guided-rest", deadline), g.Attr("data-guided-seconds", strconv.Itoa(int(seconds))), g.Attr("aria-live", "off"), c.Text(c.TextProps{Content: guidedDuration(seconds), Size: "5xl", Element: "span", ComponentProps: c.ComponentProps{Attrs: map[string]string{"data-guided-value": ""}}})),
		g.If(deadline != "", h.NoScript(guidedText(time.UnixMilli(parseDeadline(deadline)).UTC().Format(time.RFC3339)))),
		c.Flex(c.FlexProps{Wrap: true}, c.Button(c.ButtonProps{Label: p.word("add_15"), Href: add, Variant: "outline", Size: "lg", ComponentProps: c.ComponentProps{Attrs: map[string]string{"data-guided-add": ""}}}), c.Button(c.ButtonProps{Label: p.word("skip"), Href: p.nextURL(), Variant: "primary", Size: "lg", ComponentProps: c.ComponentProps{Attrs: map[string]string{"data-guided-skip": ""}}})))
	return []g.Node{
		c.Heading(c.HeadingProps{Text: p.word("rest"), Level: 1, ComponentProps: c.ComponentProps{Attrs: map[string]string{"tabindex": "-1", "data-guided-heading": ""}}}),
		c.Grid(c.GridProps{Columns: "1", MD: "2", Gap: "6"}, countdown, c.Stack(c.StackProps{Gap: "4"}, c.Heading(c.HeadingProps{Text: p.word("up_next"), Level: 2}), guidedText(nextTitle), picture)),
	}
}

func parseDeadline(value string) int64 { n, _ := strconv.ParseInt(value, 10, 64); return n }

func (p GuidedPage) finishBody() []g.Node {
	count := p.word("completed", "{completed}", strconv.Itoa(p.completed()), "{total}", strconv.Itoa(p.total()))
	if progress := p.Session.Progress; progress != nil && progress.Finished != nil {
		summary := progress.Finished
		body := []g.Node{c.Heading(c.HeadingProps{Text: summary.Title, Level: 1}), guidedText(summary.Text), guidedText(count)}
		for _, fact := range summary.Facts {
			body = append(body, guidedText(fact.Label+": "+fact.Value))
		}
		return append(body, p.link("exit", p.Exit, "primary"))
	}
	body := []g.Node{c.Heading(c.HeadingProps{Text: p.word("end"), Level: 1}), guidedText(count)}
	switch {
	case p.Policy.Finish.Callable:
		label := p.word("finish")
		if p.completed() < p.total() {
			label = p.word("finish_partial", "{completed}", strconv.Itoa(p.completed()), "{total}", strconv.Itoa(p.total()))
		}
		body = append(body, p.form("finish", label, map[string]string{}))
	case p.Policy.Finish.Declared:
		body = append(body, guidedText(p.word("finish_unavailable")))
	case p.Session.Progress == nil:
		body = append(body, guidedText(p.word("nothing_recorded")))
	}
	return append(body, p.link("continue", p.Base, "outline"))
}
