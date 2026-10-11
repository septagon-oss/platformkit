// Package contracts holds the ports modules/web declares and consumes: the two
// addresses this site links and does not serve.
package contracts

// Links is where the public site sends a visitor for the two things it does not
// do itself: signing in, and reading a file a tenant made public.
//
// Both are a composition's facts rather than this module's. The site has no
// sign-in of its own — the auth module mints the session and the shell owns the
// form — so the address belongs to whoever composed the two, and a literal in
// web would be this module naming a surface it does not serve. The file address
// is the same argument one door further on.
//
// It is Optional rather than Needs because a composition with no shell and no
// public file door is a real composition: a headless storefront that mounts the
// pages and nothing else. What it gets is a site with no sign-in link and no
// logo URL, which is what it asked for. Correctable: compose a Links.
type Links struct {
	// SignIn is where a visitor who wants to sign in is sent.
	SignIn string
	// PublicFile answers the address of a file a visitor may see. The logo is
	// the one file the site renders.
	PublicFile func(id string) string
}
