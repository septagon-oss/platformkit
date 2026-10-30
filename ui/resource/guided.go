package resource

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/septagon-oss/platformkit/kit/entity"
	c "github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/document"
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

// GuidedCapability is one session command as its screen sees it: whether the
// resource declares it at all, and whether this caller may use it. Absent,
// declared and callable are three answers a person reads differently — a declared
// command that was refused says so and offers a reload, an absent one says nothing.
type GuidedCapability struct{ Declared, Callable bool }

// GuidedPolicy is the session presentation this screen renders, in shapes ui owns.
// The ui/screens adapter resolves it and hands it over, so no renderer reaches
// past this package into the policy that produced it.
type GuidedPolicy struct {
	Complete, Finish GuidedCapability
	Labels           map[string]string
}

// GuidedPage is an authorized session and its resolved presentation. Base and
// Exit are composed local paths; Scope identifies the tenant, actor and resource.
// Mode is step, rest, finish or summary. Step/Set index a validated session.
type GuidedPage struct {
	Session                 entity.GuidedSession
	Policy                  GuidedPolicy
	Base, Exit, Scope, Mode string
	Step, Set, Extra        int
}

// Guided renders the session without deriving policy or recording progress.
// The request adapter validates the read and navigation before calling it.
func Guided(p GuidedPage, o Options) document.View {
	title := p.Session.Title
	var body []g.Node
	switch p.Mode {
	case "step":
		body = p.stepBody(o)
	case "rest":
		body = p.restBody(o)
	case "finish", "summary":
		body = p.finishBody()
	}
	config, _ := json.Marshal(struct {
		Scope     string `json:"scope"`
		Base      string `json:"base"`
		Mode      string `json:"mode"`
		Step, Set int
		Labels    map[string]string    `json:"labels"`
		Session   entity.GuidedSession `json:"session"`
	}{p.Scope, p.Base, p.Mode, p.Step, p.Set, p.Policy.Labels, p.Session})
	content := c.Stack(c.StackProps{Gap: "6"},
		c.Flex(c.FlexProps{Justify: "between", Wrap: true}, guidedText(title), p.link("exit", p.Exit, "outline")),
		g.Group(body),
		h.Section(g.Attr("data-guided-timers", ""), g.Attr("aria-label", p.word("timers"))),
		h.P(h.Role("status"), g.Attr("data-guided-status", "")),
		h.NoScript(guidedText(p.word("no_script"))), guidedText(p.word("keyboard")))
	return document.View{Title: title, Sensitive: true, Modules: []string{"js/guided.js"}, Body: []g.Node{
		h.Div(g.Attr("data-guided", string(config)), g.Attr("tabindex", "-1"), content),
	}}
}

func (p GuidedPage) word(key string, args ...string) string {
	return strings.NewReplacer(args...).Replace(p.Policy.Labels[key])
}

func guidedText(text string) g.Node { return c.Text(c.TextProps{Content: text, Size: "base"}) }
func (p GuidedPage) link(key, at, variant string) g.Node {
	return c.Button(c.ButtonProps{Label: p.word(key), Href: at, Variant: variant, Size: "lg"})
}
func (p GuidedPage) pairURL(step, set int) string {
	s := p.Session.Steps[step]
	return p.Base + "/steps/" + s.ID.String() + "/sets/" + s.Sets[set].String()
}
func (p GuidedPage) position() int {
	count := p.Set
	for _, step := range p.Session.Steps[:p.Step] {
		count += len(step.Sets)
	}
	return count
}
func (p GuidedPage) nextURL() string {
	step, set, ok := p.Session.Pair(p.position() + 1)
	if !ok {
		return p.Base + "/finish"
	}
	return p.pairURL(step, set)
}

// Rest is the pause this pair's screen may show: the seconds the session declares
// after the set under the person — between its sets, or after its last one — and
// whether that pair owns it, which only the pair at the end of the recorded prefix does.
func (p GuidedPage) Rest() (seconds int32, owned bool) {
	step := p.Session.Steps[p.Step]
	if p.Set+1 < len(step.Sets) {
		seconds = step.RestBetweenSeconds
	} else {
		seconds = step.RestAfterSeconds
	}
	return seconds, seconds > 0 && p.completed() == p.position()+1
}
func (p GuidedPage) restURL() string {
	s := p.Session.Steps[p.Step]
	return p.Base + "/rest/" + s.ID.String() + "/" + s.Sets[p.Set].String()
}
func (p GuidedPage) completed() int {
	if p.Session.Progress != nil {
		return len(p.Session.Progress.Completed)
	}
	return 0
}
func (p GuidedPage) total() int {
	total := 0
	for _, step := range p.Session.Steps {
		total += len(step.Sets)
	}
	return total
}

func (p GuidedPage) stepBody(o Options) []g.Node {
	step := p.Session.Steps[p.Step]
	progress := p.word("step", "{current}", strconv.Itoa(p.Step+1), "{total}", strconv.Itoa(len(p.Session.Steps)))
	var segments []c.Step
	count := 0
	for i, s := range p.Session.Steps {
		count += len(s.Sets)
		state, key := "upcoming", "step"
		if count <= p.completed() {
			state, key = "complete", "completed_step"
		}
		label := p.word(key, "{current}", strconv.Itoa(i+1), "{total}", strconv.Itoa(len(p.Session.Steps))) + ": " + s.Title
		segments = append(segments, c.Step{Key: s.ID.String(), Title: label, State: state, Href: p.pairURL(i, 0)})
	}
	content := []g.Node{c.Heading(c.HeadingProps{Text: step.Title, Level: 1, ComponentProps: c.ComponentProps{Attrs: map[string]string{"tabindex": "-1", "data-guided-heading": ""}}}), guidedText(step.Text)}
	if len(step.Sets) > 1 {
		content = append(content, guidedText(p.word("set", "{current}", strconv.Itoa(p.Set+1), "{total}", strconv.Itoa(len(step.Sets)))))
	}
	for _, item := range step.Items {
		content = append(content, c.Flex(c.FlexProps{Align: "center"}, guidedPicture(item.Picture, item.Label, true, o), guidedText(item.Label), guidedText(item.Quantity)))
	}
	if t := step.Target; t != nil {
		value, unit := strconv.Itoa(int(t.Value)), "target_repetitions"
		if t.Kind == "seconds" {
			value, unit = guidedDuration(t.Value), "target_duration"
		}
		content = append(content, h.Div(g.Attr("data-guided-target", ""), c.Text(c.TextProps{Content: value, Size: "5xl", Element: "span"}), guidedText(p.word(unit))))
	}
	if t := step.Timer; t != nil {
		content = append(content, c.Button(c.ButtonProps{Label: p.word("start_timer", "{name}", t.Name, "{duration}", guidedDuration(t.Seconds)), Variant: "outline", Size: "lg", ComponentProps: c.ComponentProps{Attrs: map[string]string{"data-guided-start": ""}}}))
	}
	var back []g.Node
	if p.Step > 0 {
		back = []g.Node{p.link("previous_step", p.pairURL(p.Step-1, 0), "outline")}
	}
	notice, controls := p.stepAction()
	content = append(content, notice...)
	var body g.Node = c.Stack(c.StackProps{Gap: "4"}, content...)
	if step.Picture != "" {
		body = c.Grid(c.GridProps{Columns: "1", MD: "2", Gap: "6"}, guidedPicture(step.Picture, step.Title, false, o), body)
	}
	return []g.Node{c.StepperWithSlots(c.StepperProps{Label: p.Session.Title, Segmented: true, ProgressText: progress, Steps: segments, CurrentKey: step.ID.String()}, c.StepperSlots{
		Body: []g.Node{body}, Back: back, Continue: controls, SaveExit: []g.Node{p.link("finish", p.Base+"/finish", "outline")},
	})}
}

// stepAction is a step's way forward: body text says what the control does, and the
// row of controls holds controls, because that row lays its children side by side.
func (p GuidedPage) stepAction() (notice, controls []g.Node) {
	step := p.Session.Steps[p.Step]
	if p.Policy.Complete.Callable && p.position() == p.completed() {
		label := "next_step"
		if len(step.Sets) > 1 {
			label = "done_set"
		}
		return nil, []g.Node{p.form("complete-set", p.word(label), map[string]string{"stepId": step.ID.String(), "setId": step.Sets[p.Set].String()})}
	}
	if p.Policy.Complete.Declared && !p.Policy.Complete.Callable {
		return []g.Node{guidedText(p.word("completion_unavailable"))}, []g.Node{p.link("reload", p.Base, "primary")}
	}
	next, label := p.nextURL(), "next_step"
	if p.Set+1 < len(step.Sets) {
		label = "next_set"
	}
	if _, owned := p.Rest(); owned {
		next = p.restURL()
	}
	return []g.Node{guidedText(p.word("navigation_only"))}, []g.Node{p.link(label, next, "primary")}
}

func (p GuidedPage) form(verb, label string, fields map[string]string) g.Node {
	fields["ifRevision"] = strconv.FormatInt(p.Session.Progress.Revision, 10)
	var controls []g.Node
	for _, name := range []string{"stepId", "setId", "ifRevision"} {
		if value, ok := fields[name]; ok {
			controls = append(controls, h.Input(h.Type("hidden"), h.Name(name), h.Value(value)))
		}
	}
	controls = append(controls, c.Button(c.ButtonProps{Label: label, Type: "submit", Variant: "primary", Size: "lg"}))
	return c.Form(c.FormProps{Action: p.Base + "/" + verb, Label: label}, controls...)
}

func guidedPicture(p entity.Picture, title string, decorative bool, o Options) g.Node {
	if p == "" {
		return nil
	}
	if o.ComposedSource == "" {
		return c.Media(c.MediaProps{Status: c.MediaEmpty, Reason: "—"})
	}
	src := strings.ReplaceAll(o.ComposedSource, "{reference}", string(p))
	return c.Media(c.MediaProps{Src: src, Alt: title, Decorative: decorative, Fit: "contain", Width: 640, Height: 480})
}

func guidedDuration(seconds int32) string {
	if seconds >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", seconds/3600, seconds/60%60, seconds%60)
	}
	return fmt.Sprintf("%d:%02d", seconds/60, seconds%60)
}
