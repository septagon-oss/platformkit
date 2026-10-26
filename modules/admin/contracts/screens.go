package contracts

import "github.com/septagon-oss/platformkit/kit/screen"

// Screen names a page owned by the administration shell, independent of the
// namespace in which its route is mounted. Invalid names panic.
func Screen(name string) screen.ViewRef {
	ref, err := screen.Custom("app", "admin", name)
	if err != nil {
		panic(err)
	}
	return ref
}

// Views returns fresh immutable descriptors for the composed shell providers. The
// sign-in anchors bind to the door the composition named: a shell with no auth
// module declares its sign-in page and no door. An invalid name panics.
func Views(signin Signin, roles, sessions bool) []screen.View {
	var login []screen.AnchorSpec
	if signin.OperationID != "" {
		login = append(login, field("email", "email", signin.OperationID), field("password", "password", signin.OperationID), submit("submit", signin.OperationID))
	}
	login = append(login, navigate("forgot", "forgot"))
	out := []screen.View{
		custom("dashboard", navigate("health", "health")), custom("health"), custom("tenants"), custom("gallery"),
		custom("forgot", field("email", "email", "auth-password-forgot"), submit("submit", "auth-password-forgot"), navigate("back", "login")),
		custom("set-password", field("new", "password", "auth-password-reset"), submit("submit", "auth-password-reset"), navigate("back", "login")),
		custom("verify-email", submit("submit", "auth-verify-email"), navigate("back", "login")),
	}
	if registration := signin.Registration; registration != nil {
		login = append(login, navigate("register", "register"))
		anchors := []screen.AnchorSpec{field("email", "email", registration.OperationID), field("displayName", "text", registration.OperationID)}
		if registration.Kind == KindPassword {
			anchors = append(anchors, field("password", "password", registration.OperationID), field("confirmation", "password", registration.OperationID), field("termsAccepted", "checkbox", registration.OperationID))
		}
		anchors = append(anchors, submit("submit", registration.OperationID), navigate("back", "login"))
		out = append(out, custom("register", anchors...))
	}
	if roles {
		out = append(out, custom("roles", field("name", "text", "admin-role-set"), submit("create", "admin-role-set")))
	}
	if sessions {
		out = append(out, custom("sessions", submit("revoke-rest", "admin-sessions-revoke-rest")))
	}
	return append(out, custom("login", login...))
}

func custom(name string, anchors ...screen.AnchorSpec) screen.View {
	return screen.MustView(screen.ViewSpec{Ref: Screen(name), Cardinality: screen.One, Anchors: anchors})
}

func field(name, control, operation string) screen.AnchorSpec {
	return screen.AnchorSpec{Name: "field." + name, Control: control, Binding: screen.Value(operation, []string{name}), Interactions: []screen.Interaction{screen.Committed}}
}

func submit(name, operation string) screen.AnchorSpec {
	return screen.AnchorSpec{Name: "action." + name, Control: "button", Binding: screen.Submission(operation), Interactions: []screen.Interaction{screen.Activated, screen.Submitted}}
}

func navigate(name, destination string) screen.AnchorSpec {
	return screen.AnchorSpec{Name: "action." + name, Control: "link", Binding: screen.Navigation(Screen(destination)), Interactions: []screen.Interaction{screen.Activated}}
}
