package components

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

type ChartPoint struct {
	Key   string   `json:"key"`
	X     float64  `json:"x"`
	Y     *float64 `json:"y,omitempty"`
	XText string   `json:"xText"`
	YText string   `json:"yText"`
}
type ChartSeries struct {
	Key     string       `json:"key"`
	Label   string       `json:"label"`
	Tone    string       `json:"tone,omitempty"`
	Pattern string       `json:"pattern,omitempty" enum:",solid,dash,dot"`
	Points  []ChartPoint `json:"points"`
}
type Tick struct {
	Value float64 `json:"value"`
	Text  string  `json:"text"`
}
type Bar struct {
	Key       string  `json:"key"`
	Label     string  `json:"label"`
	Value     float64 `json:"value"`
	ValueText string  `json:"valueText"`
	Tone      string  `json:"tone,omitempty"`
}
type SparklineProps struct {
	ComponentProps
	Label      string       `json:"label"`
	State      ContentState `json:"state,omitzero"`
	Summary    string       `json:"summary,omitempty"`
	Points     []ChartPoint `json:"points,omitempty"`
	Tone       string       `json:"tone,omitempty"`
	Decorative bool         `json:"decorative,omitzero"`
}
type AreaChartProps struct {
	ComponentProps
	Label         string        `json:"label"`
	State         ContentState  `json:"state,omitzero"`
	Description   string        `json:"description,omitempty"`
	Series        []ChartSeries `json:"series,omitempty"`
	XLabel        string        `json:"xLabel"`
	YLabel        string        `json:"yLabel"`
	XTicks        []Tick        `json:"xTicks,omitempty"`
	YTicks        []Tick        `json:"yTicks,omitempty"`
	Ranges        []ChoiceLink  `json:"ranges,omitempty"`
	TableLabel    string        `json:"tableLabel"`
	ShowDataLabel string        `json:"showDataLabel"`
}
type BarChartProps struct {
	ComponentProps
	Label         string       `json:"label"`
	State         ContentState `json:"state,omitzero"`
	Description   string       `json:"description,omitempty"`
	Bars          []Bar        `json:"bars,omitempty"`
	AxisLabel     string       `json:"axisLabel"`
	Ticks         []Tick       `json:"ticks,omitempty"`
	TableLabel    string       `json:"tableLabel"`
	ShowDataLabel string       `json:"showDataLabel"`
}
type StatTileProps struct {
	ComponentProps
	Label          string       `json:"label"`
	State          ContentState `json:"state,omitzero"`
	ValueText      string       `json:"valueText,omitempty"`
	Description    string       `json:"description,omitempty"`
	DeltaText      string       `json:"deltaText,omitempty"`
	DeltaDirection string       `json:"deltaDirection,omitempty" enum:",up,down,flat,unknown"`
	DeltaTone      string       `json:"deltaTone,omitempty"`
	ComparisonText string       `json:"comparisonText,omitempty"`
	Href           string       `json:"href,omitempty"`
}

type ChartBounds struct{ MinX, MaxX, MinY, MaxY float64 }

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func expandBounds(lo, hi float64, zero bool) (float64, float64, error) {
	if zero {
		lo = min(lo, 0)
		hi = max(hi, 0)
	}
	if lo == hi {
		if zero && lo == 0 {
			hi = 1
		} else {
			delta := max(math.Abs(lo)*0.05, 1)
			lo -= delta
			hi += delta
		}
	}
	if !finite(hi-lo) || hi <= lo {
		return 0, 0, fmt.Errorf("Chart: nonfinite derived span")
	}
	return lo, hi, nil
}
func validatePoints(points []ChartPoint) error {
	ids := map[string]bool{}
	for i, point := range points {
		if !required(point.Key, point.XText, point.YText) || ids[point.Key] || !finite(point.X) || point.Y != nil && !finite(*point.Y) || i > 0 && point.X <= points[i-1].X {
			return fmt.Errorf("Chart: unique labels, finite values and increasing X required")
		}
		ids[point.Key] = true
	}
	return nil
}

// ChartRange computes bounds without sorting or filling gaps. False means no
// finite Y was supplied; callers render the missing values without invented axes.
func ChartRange(points []ChartPoint, includeZero bool) (ChartBounds, bool, error) {
	if err := validatePoints(points); err != nil {
		return ChartBounds{}, false, err
	}
	return chartRange(points, includeZero)
}
func chartRange(points []ChartPoint, includeZero bool) (ChartBounds, bool, error) {
	b := ChartBounds{MinX: math.Inf(1), MaxX: math.Inf(-1), MinY: math.Inf(1), MaxY: math.Inf(-1)}
	found := false
	for _, point := range points {
		if point.Y == nil {
			continue
		}
		found = true
		b.MinX = min(b.MinX, point.X)
		b.MaxX = max(b.MaxX, point.X)
		b.MinY = min(b.MinY, *point.Y)
		b.MaxY = max(b.MaxY, *point.Y)
	}
	if !found {
		return ChartBounds{}, false, nil
	}
	var err error
	b.MinX, b.MaxX, err = expandBounds(b.MinX, b.MaxX, false)
	if err != nil {
		return ChartBounds{}, false, err
	}
	b.MinY, b.MaxY, err = expandBounds(b.MinY, b.MaxY, includeZero)
	return b, err == nil, err
}

// ChartPosition maps to a normalized 100 by 100 SVG plot, with inverted Y.
func ChartPosition(bounds ChartBounds, x, y float64) (float64, float64, error) {
	if !finite(x) || !finite(y) || !finite(bounds.MaxX-bounds.MinX) || !finite(bounds.MaxY-bounds.MinY) || bounds.MaxX <= bounds.MinX || bounds.MaxY <= bounds.MinY {
		return 0, 0, fmt.Errorf("Chart: invalid scale")
	}
	px, py := (x-bounds.MinX)/(bounds.MaxX-bounds.MinX)*100, 100-(y-bounds.MinY)/(bounds.MaxY-bounds.MinY)*100
	if !finite(px) || !finite(py) {
		return 0, 0, fmt.Errorf("Chart: nonfinite coordinate")
	}
	return px, py, nil
}
func validateTicks(ticks []Tick, lo, hi float64) error {
	for i, tick := range ticks {
		if !finite(tick.Value) || tick.Value < lo || tick.Value > hi || !required(tick.Text) || i > 0 && tick.Value <= ticks[i-1].Value {
			return fmt.Errorf("Chart: ticks must be labeled, increasing and inside the range")
		}
	}
	return nil
}
func chartSVG(label string, decorative bool, children ...g.Node) g.Node {
	attrs := []g.Node{g.Attr("viewBox", "-5 -5 110 110"), g.Attr("width", "400"), g.Attr("height", "220"), g.Attr("preserveAspectRatio", "none"), g.Attr("data-chart-plot", ""), h.Class(clDataCount.Compile())}
	if decorative {
		attrs = append(attrs, g.Attr("aria-hidden", "true"))
	} else {
		attrs = append(attrs, h.Role("img"), g.Attr("aria-label", label), g.El("title", g.Text(label)))
	}
	return g.El("svg", append(attrs, children...)...)
}
func chartPaths(points []ChartPoint, b ChartBounds, area bool, pattern string) []g.Node {
	var paths []g.Node
	var segment []string
	flush := func() {
		if len(segment) == 0 {
			return
		}
		if len(segment) == 1 {
			xy := strings.Split(segment[0], ",")
			paths = append(paths, g.El("circle", g.Attr("cx", xy[0]), g.Attr("cy", xy[1]), g.Attr("r", "1.5"), g.Attr("fill", "currentColor")))
		} else {
			d := "M" + strings.Join(segment, " L")
			if area {
				_, zero, _ := ChartPosition(b, b.MinX, 0)
				first := strings.Split(segment[0], ",")[0]
				last := strings.Split(segment[len(segment)-1], ",")[0]
				fill := d + fmt.Sprintf(" L%s,%g L%s,%g Z", last, zero, first, zero)
				paths = append(paths, g.El("path", g.Attr("d", fill), g.Attr("fill", "currentColor"), g.Attr("opacity", "0.15")))
			}
			attrs := []g.Node{g.Attr("d", d), g.Attr("fill", "none"), g.Attr("stroke", "currentColor"), g.Attr("stroke-width", "1.5"), g.Attr("vector-effect", "non-scaling-stroke")}
			if pattern == "dash" {
				attrs = append(attrs, g.Attr("stroke-dasharray", "6 3"))
			}
			if pattern == "dot" {
				attrs = append(attrs, g.Attr("stroke-dasharray", "1 3"))
			}
			paths = append(paths, g.El("path", attrs...))
		}
		segment = nil
	}
	for _, point := range points {
		if point.Y == nil {
			flush()
			continue
		}
		x, y, _ := ChartPosition(b, point.X, *point.Y)
		segment = append(segment, fmt.Sprintf("%g,%g", x, y))
	}
	flush()
	return paths
}
func (p SparklineProps) Validate() error {
	if err := aggregateState(p.Label, p.State, len(p.Points) > 0 || p.Summary != ""); err != nil {
		return err
	}
	if !p.State.ready() {
		return nil
	}
	if !required(p.Summary) {
		return fmt.Errorf("Sparkline: summary required")
	}
	_, _, err := ChartRange(p.Points, false)
	return err
}
func Sparkline(p SparklineProps) g.Node { return SparklineWithSlots(p, StateSlots{}) }
func SparklineWithSlots(p SparklineProps, slots StateSlots) g.Node {
	if err := p.Validate(); err != nil {
		return invalidComponent(err)
	}
	if !p.State.ready() {
		return sharedSection(p.ComponentProps, "sparkline", p.Label, stateBody(p.State, slots))
	}
	b, found, _ := ChartRange(p.Points, false)
	var plot g.Node
	if found {
		plot = chartSVG(p.Label, p.Decorative, g.El("g", append([]g.Node{g.Attr("data-chart-tone", p.Tone)}, chartPaths(p.Points, b, false, "")...)...))
	}
	return sharedSection(p.ComponentProps, "sparkline", p.Label, stateBody(p.State, slots), plot, Text(TextProps{Content: p.Summary, Size: "sm"}))
}
func (p AreaChartProps) Validate() error {
	if err := aggregateState(p.Label, p.State, len(p.Series) > 0 || len(p.XTicks) > 0 || len(p.YTicks) > 0 || p.Description != ""); err != nil {
		return err
	}
	if !p.State.ready() {
		if retainedNavigation(p.State.Status, p.Ranges) {
			return fmt.Errorf("AreaChart: absent content must clear navigation into the previous result")
		}
		return nil
	}
	if !required(p.Description, p.XLabel, p.YLabel, p.TableLabel, p.ShowDataLabel) {
		return fmt.Errorf("AreaChart: accessible description and labels required")
	}
	ids := map[string]bool{}
	var all []ChartPoint
	for _, series := range p.Series {
		if !required(series.Key, series.Label) || ids[series.Key] {
			return fmt.Errorf("AreaChart: unique labeled series required")
		}
		ids[series.Key] = true
		switch series.Pattern {
		case "", "solid", "dash", "dot":
		default:
			return fmt.Errorf("AreaChart: unknown pattern")
		}
		if err := validatePoints(series.Points); err != nil {
			return err
		}
		all = append(all, series.Points...)
	}
	b, found, err := chartRange(all, true)
	if err != nil {
		return err
	}
	if found {
		if err := validateTicks(p.XTicks, b.MinX, b.MaxX); err != nil {
			return err
		}
		if err := validateTicks(p.YTicks, b.MinY, b.MaxY); err != nil {
			return err
		}
	} else if len(p.XTicks)+len(p.YTicks) > 0 {
		return fmt.Errorf("AreaChart: absent values must not carry axes")
	}
	return validateChoices(p.Ranges)
}
func AreaChart(p AreaChartProps) g.Node { return AreaChartWithSlots(p, StateSlots{}) }
func AreaChartWithSlots(p AreaChartProps, slots StateSlots) g.Node {
	if err := p.Validate(); err != nil {
		return invalidComponent(err)
	}
	if !p.State.ready() {
		return sharedSection(p.ComponentProps, "area-chart", p.Label, stateBody(p.State, slots))
	}
	var all []ChartPoint
	for _, series := range p.Series {
		all = append(all, series.Points...)
	}
	b, found, _ := chartRange(all, true)
	var paths, tables []g.Node
	for _, series := range p.Series {
		if found {
			paths = append(paths, g.El("g", append([]g.Node{g.Attr("data-chart-tone", series.Tone)}, chartPaths(series.Points, b, true, series.Pattern)...)...))
		}
		var rows []TableRow
		for _, point := range series.Points {
			rows = append(rows, TableRow{ID: point.Key, Cells: map[string]any{"x": point.XText, "y": point.YText}})
		}
		tables = append(tables, Heading(HeadingProps{Text: series.Label, Level: 3, Size: 5}), Table(TableProps{Label: p.TableLabel + " · " + series.Label, Columns: []TableColumn{{Key: "x", Label: p.XLabel, RowHeader: true}, {Key: "y", Label: p.YLabel}}, Rows: rows}))
	}
	var plot g.Node
	if found {
		for _, tick := range p.YTicks {
			_, y, _ := ChartPosition(b, b.MinX, tick.Value)
			paths = append(paths, g.El("text", g.Attr("x", "0"), g.Attr("y", strconv.FormatFloat(y, 'g', -1, 64)), g.Attr("font-size", "4"), g.Text(tick.Text)))
		}
		for _, tick := range p.XTicks {
			x, _, _ := ChartPosition(b, tick.Value, b.MinY)
			paths = append(paths, g.El("text", g.Attr("x", strconv.FormatFloat(x, 'g', -1, 64)), g.Attr("y", "100"), g.Attr("font-size", "4"), g.Text(tick.Text)))
		}
		plot = chartSVG(p.Description, false, paths...)
	}
	return sharedSection(p.ComponentProps, "area-chart", p.Label, stateBody(p.State, slots), Heading(HeadingProps{Text: p.Label, Level: 2, Size: 4}), Text(TextProps{Content: p.Description, Size: "sm"}), choiceLinks(p.Ranges, p.Disabled), plot, h.Details(h.Summary(h.Class(clDataDisclosure.Compile()), g.Text(p.ShowDataLabel)), g.Group(tables)))
}
func barRange(bars []Bar) (float64, float64, error) {
	lo, hi := 0.0, 0.0
	for _, bar := range bars {
		lo = min(lo, bar.Value)
		hi = max(hi, bar.Value)
	}
	return expandBounds(lo, hi, true)
}
func (p BarChartProps) Validate() error {
	if err := aggregateState(p.Label, p.State, len(p.Bars) > 0 || len(p.Ticks) > 0 || p.Description != ""); err != nil {
		return err
	}
	if !p.State.ready() {
		return nil
	}
	if !required(p.Description, p.AxisLabel, p.TableLabel, p.ShowDataLabel) {
		return fmt.Errorf("BarChart: accessible description and labels required")
	}
	ids := map[string]bool{}
	for _, bar := range p.Bars {
		if !required(bar.Key, bar.Label, bar.ValueText) || ids[bar.Key] || !finite(bar.Value) {
			return fmt.Errorf("BarChart: unique labeled finite bars required")
		}
		ids[bar.Key] = true
	}
	lo, hi, err := barRange(p.Bars)
	if err != nil {
		return err
	}
	return validateTicks(p.Ticks, lo, hi)
}
func BarChart(p BarChartProps) g.Node { return BarChartWithSlots(p, StateSlots{}) }
func BarChartWithSlots(p BarChartProps, slots StateSlots) g.Node {
	if err := p.Validate(); err != nil {
		return invalidComponent(err)
	}
	if !p.State.ready() {
		return sharedSection(p.ComponentProps, "bar-chart", p.Label, stateBody(p.State, slots))
	}
	lo, hi, _ := barRange(p.Bars)
	b := ChartBounds{MinX: 0, MaxX: 100, MinY: lo, MaxY: hi}
	_, zero, _ := ChartPosition(b, 0, 0)
	var shapes []g.Node
	var rows []TableRow
	for i, bar := range p.Bars {
		_, y, _ := ChartPosition(b, 0, bar.Value)
		width := 100 / float64(len(p.Bars))
		shapes = append(shapes, g.El("rect", g.Attr("data-chart-tone", bar.Tone), g.Attr("x", fmt.Sprint(float64(i)*width+width*0.1)), g.Attr("y", fmt.Sprint(min(y, zero))), g.Attr("width", fmt.Sprint(width*0.8)), g.Attr("height", fmt.Sprint(math.Abs(y-zero))), g.Attr("fill", "currentColor")))
		rows = append(rows, TableRow{ID: bar.Key, Cells: map[string]any{"label": bar.Label, "value": bar.ValueText}})
	}
	for _, tick := range p.Ticks {
		_, y, _ := ChartPosition(b, 0, tick.Value)
		shapes = append(shapes, g.El("text", g.Attr("x", "0"), g.Attr("y", fmt.Sprint(y)), g.Attr("font-size", "4"), g.Text(tick.Text)))
	}
	var plot g.Node
	if len(p.Bars) > 0 {
		plot = chartSVG(p.Description, false, shapes...)
	}
	return sharedSection(p.ComponentProps, "bar-chart", p.Label, stateBody(p.State, slots), Heading(HeadingProps{Text: p.Label, Level: 2, Size: 4}), Text(TextProps{Content: p.Description, Size: "sm"}), plot,
		h.Details(h.Summary(h.Class(clDataDisclosure.Compile()), g.Text(p.ShowDataLabel)), Table(TableProps{Label: p.TableLabel, Columns: []TableColumn{{Key: "label", Label: p.TableLabel, RowHeader: true}, {Key: "value", Label: p.AxisLabel}}, Rows: rows})))
}
func (p StatTileProps) Validate() error {
	if err := aggregateState(p.Label, p.State, p.ValueText != "" || p.Description != "" || p.DeltaText != "" || p.ComparisonText != "" || p.Href != ""); err != nil {
		return err
	}
	if !p.State.ready() {
		return nil
	}
	if !required(p.ValueText) {
		return fmt.Errorf("StatTile: displayed value required")
	}
	switch p.DeltaDirection {
	case "", "up", "down", "flat", "unknown":
		return nil
	}
	return fmt.Errorf("StatTile: unknown direction")
}
func StatTile(p StatTileProps) g.Node { return StatTileWithSlots(p, StateSlots{}) }
func StatTileWithSlots(p StatTileProps, slots StateSlots) g.Node {
	if err := p.Validate(); err != nil {
		return invalidComponent(err)
	}
	if !p.State.ready() {
		return sharedSection(p.ComponentProps, "stat-tile", p.Label, stateBody(p.State, slots))
	}
	var value g.Node = Heading(HeadingProps{Text: p.ValueText, Level: 3, Size: 2})
	if p.Href != "" {
		value = navigationLink(p.Disabled, p.Href, value)
	}
	arrow := map[string]string{"up": "↑", "down": "↓", "flat": "→", "unknown": "·"}[p.DeltaDirection]
	return sharedSection(p.ComponentProps, "stat-tile", p.Label, stateBody(p.State, slots), Heading(HeadingProps{Text: p.Label, Level: 2, Size: 5}), value, Text(TextProps{Content: p.Description, Size: "sm"}),
		g.If(p.DeltaText != "", Flex(FlexProps{Gap: "2", Align: "center"}, h.Span(g.Attr("aria-hidden", "true"), g.Text(arrow)), Badge(BadgeProps{Label: p.DeltaText, Tone: p.DeltaTone}))), Text(TextProps{Content: p.ComparisonText, Size: "sm"}))
}
