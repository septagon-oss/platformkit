package contracts

// ConfirmationChrome is the chrome of the one page this module writes for
// itself: the sheet the emailed confirmation link opens.
//
// It is a port the composition answers, and it holds two addresses and nothing
// else, because that is the whole of what the module cannot know. Which palette
// the page is drawn in and which words it speaks are the resolved composition's
// answer (pkit.Skin), which the module reads for itself; where the installation
// serves its stylesheet and where its sign-in page is are facts about mounts this
// module neither owns nor serves, and a module that named them would be naming a
// surface it does not serve.
//
// The zero value asks for nothing: with both fields empty the page renders its
// own document with no stylesheet link and no way back, which is what an empty
// composition always meant. An application that mounts the sheet and the shell
// says so here — apps/platformkit does, with the two addresses its own mounts
// answer at, and TestTheEmailedLinkOpensAPageThatConfirms asks the running server
// that the page the link opens links the sheet it was given.
type ConfirmationChrome struct {
	// Assets is the address the installation's stylesheet is served at: the
	// document links Assets + "/app.css", so it is the mount one server serves
	// and not a second copy of the sheet.
	Assets string
	// SignIn is where a person who has just confirmed their address goes, and
	// the one way back the page offers.
	SignIn string
}
