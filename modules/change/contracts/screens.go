package contracts

import "github.com/septagon-oss/platformkit/kit/screen"

// Screen names the queue or a single proposal independently of a record ID.
func Screen(name string) screen.ViewRef {
	ref, err := screen.Custom("app", "change", name)
	if err != nil {
		panic(err)
	}
	return ref
}

// Views declares the composed review pages and their unique decision controls.
// Fixed verdicts distinguish decisions submitted through the same operation.
func Views(enabled bool) []screen.View {
	if !enabled {
		return nil
	}
	fixed := func(value string) screen.FixedInput {
		return screen.FixedInput{Location: "form", FieldPath: []string{"verdict"}, Value: value}
	}
	anchors := []screen.AnchorSpec{{Name: "field.comment", Control: "text", Binding: screen.Value("screen-change-proposal-review", []string{"comment"}, fixed("declined")), Interactions: []screen.Interaction{screen.Committed}}}
	for _, decision := range []struct{ name, operation, verdict string }{
		{"approve", "screen-change-proposal-review", "approved"}, {"decline", "screen-change-proposal-review", "declined"},
		{"apply", "screen-change-proposal-apply", ""}, {"withdraw", "screen-change-proposal-withdraw", ""},
	} {
		inputs := []screen.FixedInput{}
		if decision.verdict != "" {
			inputs = append(inputs, fixed(decision.verdict))
		}
		anchors = append(anchors, screen.AnchorSpec{Name: "action." + decision.name, Control: "button", Binding: screen.Submission(decision.operation, inputs...), Interactions: []screen.Interaction{screen.Activated, screen.Submitted}})
	}
	return []screen.View{
		screen.MustView(screen.ViewSpec{Ref: Screen("queue"), Cardinality: screen.One}),
		screen.MustView(screen.ViewSpec{Ref: Screen("proposal"), Cardinality: screen.One, Anchors: anchors}),
	}
}
