package screens

// Connection is the document a shell reads before anybody has signed in: what
// this workspace is called, what it looks like, and how a person may get in.
//
// It lives beside Catalog for the reason Catalog does — the type is what reaches
// the published OpenAPI document, and a body handed over as `any` leaves the
// fields out of it, which is the hole TestTheCatalogOperationDescribesWhatAShellParses
// was written to refuse. It is values only, because ui may not import a module
// (scripts/check_packages.sh): the builder that fills these fields from four
// owners is the composition's, and lives in apps/platformkit/connection.go.
//
// There is no version field. CatalogVersion exists because a shell is already
// installed parsing that document and needs to know when a key it reads changed
// meaning; this document has no installed reader yet, so the rule
// (catalog.go: "additive, optional, and never a change of meaning") has nothing
// to govern until the first shell is written against it. Adding the number now
// would be a field nothing reads.
//
// Everything in it is what an anonymous visitor to that host may already see in
// the HTML of its public page — the name, the mark, the accent — plus the doors
// the installation mounted, which are not a secret either: a form nobody may
// reach is only discoverable by being posted to. Nothing about a person, a
// count or a plan is here, and TestTheConnectionDocumentCarriesNoPrivateData
// refuses the addition by key.
type Connection struct {
	// Name is the workspace's own word for itself: its title, else the tenant's
	// name, else the installation's brand — the same rule the public page's header
	// applies (modules/web/internal/mount.go). It is never empty, so a shell never
	// has to invent a name for the screen it is drawing.
	Name string `json:"name" doc:"What this workspace calls itself" example:"Acme"`
	// LogoURL is the mark, as an absolute path on this server with no host in it,
	// exactly like every other address in this document: a device fills in the
	// server it was told about. Omitted when this workspace has no mark, and when
	// the mark it stored is not a file this server would hand to a caller with no
	// session — an address that answers 404 is worse than no address to a shell that
	// draws what it is told. Never an empty string, because an empty src is a request
	// against the document's own address.
	LogoURL string `json:"logoUrl,omitempty" doc:"Path of this workspace's mark, when it has one" example:"/api/v1/public/file/files/6f2b3a1e-1c3a-4a51-9d1a-2f0f3c4b5a69"`
	// Accent is the workspace's colour, #rrggbb, byte-for-byte the value the web
	// page pins as --pk-color-accent-default. Never a corrected value: a colour
	// that reads badly is refused at the write, and what is stored is what is
	// served (see modules/site/contracts/contrast.go for the rule and why it
	// rejects rather than adjusts).
	Accent string `json:"accent" doc:"Brand accent, #rrggbb" example:"#2563eb"`
	// AccentRatio quotes how that colour reads against the kit's two canvases, in
	// hundredths. A shell is told the number rather than computing it per platform,
	// and `theme` says which of the two it is drawing on right now. Both are
	// answered even for a tenant whose stored colour predates the rule, which is
	// how an administrator can see the problem they already have.
	AccentRatio AccentRatio `json:"accentRatio" doc:"How the accent reads against the light and dark canvases"`
	// Theme is the scheme this workspace asked for: light, dark, or system to
	// follow the visitor. It travels with the ratios so a shell on either canvas
	// picks the one that belongs to it.
	Theme string `json:"theme" enum:"light,dark,system" doc:"Colour scheme, or system to follow the visitor's" example:"system"`
	// Revision is this workspace's own write count, from 1. It is the shell's cache
	// key and its cheap revalidation: `no-store` means the bytes may not be handed
	// to a shared cache, but a shell may re-read and compare this number. Zero means
	// nobody has ever configured this workspace.
	Revision int64 `json:"revision" doc:"This workspace's settings write count, from 1" example:"3"`

	// Methods is what this server will accept today, as four independent
	// booleans. It never claims exclusivity: a tenant with OIDC keeps its
	// passwords working, and a document that said otherwise would be a shell
	// hiding a door that still opens.
	Methods SignInMethods `json:"methods" doc:"Which sign-in methods this workspace accepts now"`
	// SignIn is the door and the form the installation mounted, verbatim from its
	// own composition, so a shell never offers a form the installation did not
	// mount.
	SignIn SignInDoor `json:"signIn" doc:"The sign-in door this installation mounted"`
	// Recovery is whether a forgotten password can be recovered here. Both halves
	// matter: no mail server means the mail goes nowhere, and a workspace with no
	// password door has no recovery to offer.
	Recovery RecoveryAvailability `json:"recovery" doc:"Whether a forgotten password can be recovered here"`
}

// AccentRatio is the WCAG contrast ratio of the workspace's accent against each
// of the kit's two canvases, in hundredths. The numbers are the server's, not a
// guess: they come from the same arithmetic the write-time rule compared
// (site.MinAccentRatio is the threshold they are measured against).
type AccentRatio struct {
	Light float64 `json:"light" doc:"Ratio against the light canvas" example:"4.50"`
	Dark  float64 `json:"dark" doc:"Ratio against the dark canvas" example:"3.55"`
}

// SignInMethods is one boolean per protocol, each answering on its own.
type SignInMethods struct {
	// Password is whether this installation mounted the password door at all.
	Password bool `json:"password" doc:"Whether a password is accepted here"`
	// Passkey is this workspace's own answer at the usernameless door
	// (auth.SetPasskeySignIn), which is false until somebody turns it on.
	Passkey bool `json:"passkey" doc:"Whether a passkey alone signs in here"`
	// OIDC and SAML are this workspace's own provider rows. A provider whose
	// secret cannot be resolved reads as no provider, so nothing secretable is
	// ever named here.
	OIDC bool `json:"oidc" doc:"Whether an OpenID Connect provider is configured for this workspace"`
	SAML bool `json:"saml" doc:"Whether a SAML provider is configured for this workspace"`
}

// SignInDoor is the composition's own sign-in value: the address of the door and,
// when the installation opened one, the registration form beside it. It is a copy
// of what the server mounted rather than a description of it, which is what makes
// it safe for a shell to render.
type SignInDoor struct {
	// Address is the door a set of credentials is posted to.
	Address string `json:"address" doc:"Where credentials are posted" example:"/api/v1/auth/login"`
	// Registration is the sign-up form, absent when this installation opens none.
	Registration *RegistrationDoor `json:"registration,omitempty"`
}

// RegistrationDoor is the sign-up form as the composition declared it.
type RegistrationDoor struct {
	// Kind is which form the shell renders, spelled as the admin shell spells it.
	Kind string `json:"kind" doc:"Which registration form to render" example:"password"`
	// Address is the door that form posts to.
	Address string `json:"address" doc:"Where the registration form posts" example:"/api/v1/public/auth/register"`
}

// RecoveryAvailability is the one answer a forgot-password control needs.
type RecoveryAvailability struct {
	// Available is true only when a recovery mail could actually leave this
	// installation and this workspace accepts a password.
	Available bool `json:"available" doc:"Whether a forgotten password can be recovered here"`
}
