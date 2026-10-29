package contracts

import "github.com/septagon-oss/platformkit/kit/screen"

// Screen names a stable library page; repeated file tiles have no anchors.
func Screen(name string) screen.ViewRef {
	ref, err := screen.Custom("app", "file", name)
	if err != nil {
		panic(err)
	}
	return ref
}

// Views declares only the controls whose providers are composed. Every call
// returns independent values; records and permissions never enter this inventory.
func Views(enabled, upload, removal bool) []screen.View {
	if !enabled {
		return nil
	}
	anchors := []screen.AnchorSpec{}
	field := func(name, control, operation string, fixed ...screen.FixedInput) {
		anchors = append(anchors, screen.AnchorSpec{Name: "field." + name, Control: control, Binding: screen.Value(operation, []string{name}, fixed...), Interactions: []screen.Interaction{screen.Committed}})
	}
	submit := func(name, operation string, fixed ...screen.FixedInput) {
		anchors = append(anchors, screen.AnchorSpec{Name: "action." + name, Control: "button", Binding: screen.Submission(operation, fixed...), Interactions: []screen.Interaction{screen.Activated, screen.Submitted}})
	}
	field("q", "search", "file-library")
	field("type", "select", "file-library")
	field("sort", "select", "file-library")
	field("unused", "checkbox", "file-library")
	submit("filter", "file-library")
	anchors = append(anchors, screen.AnchorSpec{Name: "action.clear", Control: "link", Binding: screen.Navigation(Screen("library")), Interactions: []screen.Interaction{screen.Activated}})
	if upload {
		fixed := screen.FixedInput{Location: "query", FieldPath: []string{"image"}, Value: "true"}
		field("file", "file", "file-library-upload", fixed)
		submit("upload", "file-library-upload", fixed)
	}
	field("alt", "textarea", "file-library-alt")
	field("decorative", "checkbox", "file-library-alt")
	submit("save-alt", "file-library-alt")
	if removal {
		submit("delete", "file-library-delete")
	}
	return []screen.View{
		screen.MustView(screen.ViewSpec{Ref: Screen("library"), Cardinality: screen.One, Anchors: anchors}),
		screen.MustView(screen.ViewSpec{Ref: Screen("pick"), Cardinality: screen.One}),
	}
}
