package contracts

import "github.com/septagon-oss/platformkit/kit/screen"

// VerifyEmailOperation is the id of the POST the confirmation page submits to.
// The anchor below names it and the route beside it declares it, so the two
// cannot drift apart and one submission keeps one identity.
const VerifyEmailOperation = "auth-verify-email-confirm"

// Screen names a page this module writes for itself, on the public face its
// route is mounted from. Invalid names panic.
func Screen(name string) screen.ViewRef {
	ref, err := screen.Custom("public", "auth", name)
	if err != nil {
		panic(err)
	}
	return ref
}

// Views declares the page a verification mail opens, and nothing at all where
// no such page is mounted: a declared view with no mounted response is refused.
// Every call returns fresh values, and no record or permission enters them.
func Views(mounted bool) []screen.View {
	if !mounted {
		return nil
	}
	return []screen.View{screen.MustView(screen.ViewSpec{
		Ref: Screen("verify-email"), Cardinality: screen.One,
		Anchors: []screen.AnchorSpec{{
			Name: "action.confirm", Control: "button",
			Binding:      screen.Submission(VerifyEmailOperation),
			Interactions: []screen.Interaction{screen.Activated, screen.Submitted},
		}},
	})}
}
