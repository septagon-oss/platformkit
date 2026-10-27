// Package notification is the module manifest: telling somebody something, in
// the application and optionally by mail.
//
// It is the shape every module follows, with one deliberate omission: there is
// no rest.Spec. A Spec's list route is the whole tenant, and these rows are
// addressed to a person — every caller's list is a different list — so the two
// routes are written by hand and scoped by the principal rather than by a
// permission. See internal/handler.go.
package notification

import (
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/push"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/internal"
	"github.com/septagon-oss/platformkit/ui/page"
)

// Deps is what this module cannot make for itself.
type Deps struct {
	// Recipients turns a user id into an email address. The interface is
	// declared in this module's contracts/ and satisfied by an adapter over the
	// user module in apps/platformkit: the app adapts, so notification never
	// names user. A nil lookup writes every row and sends no mail.
	Recipients contracts.RecipientLookup

	// Senders is the tenant's own sending address — its name, its domain, its
	// DKIM pair and what proved them. The product builds it over its own
	// configuration, because what proves a domain (a TXT record read by a
	// resolver, an operator who looked, an installation with no egress at all) is
	// the product's fact and not this module's.
	//
	// Nil is the other answer and it is not "no mail": it means this installation
	// speaks as the address in its own configuration for every tenant it serves,
	// which is what it did before per-tenant senders existed, and the decision
	// says so rather than pretending the tenant chose it.
	Senders contracts.Senders

	// Providers are the carriers beyond mail — VAPID for a browser, an
	// adapter for a phone, an HTTP client for a tenant's endpoint. Each one is
	// carried by a subscription the manifest composes for it (internal.Carrier),
	// named by the same event Notify asks that channel by, so a channel this list
	// holds reaches its carrier and closes its ledger row with sent, failed or a
	// suppression. A channel with no provider here is suppressed with a reason
	// that names the deployment. Either way the ledger accounts for every channel
	// a notice asked for, whatever this list holds — which is what
	// delivery_ledger_coverage measures and what it would stop measuring the
	// moment a provider were wired to nothing. Mail is not one of these: the
	// module's own worker carries it, which is what Deps.Mailer is for, and
	// wiring a provider for mail or for in-app panics where the mistake is.
	Providers contracts.Providers

	// Mailer sends the rendered message, in the worker.
	//
	// nil means this installation has no mail transport at all: the deployment
	// picked notification.Module's "none" because the configuration names no
	// server and did not ask for the in-memory sink either. It is a value rather
	// than a panic because "no mail server here" is a state a real deployment is
	// in, and the module's job in that state is to say so on the record: every
	// notice is still written and still read in the application, and the mail each
	// one asks for is recorded as suppressed with the reason, which is the delivery
	// ledger's own vocabulary rather than a log line somebody has to notice. What
	// refuses outright is a command whose promise is the message itself
	// (modules/auth's emailed verification link): that one answers a reasoned 503
	// and writes nothing, because there is no ledger a stranger can read.
	Mailer contracts.Mailer

	// Hosts turns the tenant an event belongs to into the host its people reach
	// the application at, which is what a notice's link has to become for a mail
	// client. It is declared in this module's contracts/ and satisfied by an
	// adapter over the tenant module in apps/platformkit, the same way
	// Recipients is satisfied over the user module.
	Hosts contracts.HostLookup

	// Secure says the application is reached over https, which is the scheme a
	// mailed link is built with. It is one bool rather than a host because it is
	// the same decision the session cookie's Secure flag is: a laptop reached at
	// a local name gets http, and everything else gets https.
	Secure bool

	// Push is what this installation can carry to a device, one adapter per
	// platform (kit/push). It is how the two device channels get carried: a
	// platform present here opens its channel — every phone family under push,
	// the browser under web_push — through one carrier this manifest composes
	// (internal.PushCarrier), which reads the recipient's registered devices and
	// closes the ledger row around what the provider answers.
	//
	// Nil, or a map with no adapter that reaches a channel, is not "broken": it
	// is the same sentence a missing key in Providers says, so Decide suppresses
	// the channel with a reason naming the deployment and no subscription waits
	// on an event nobody will publish. The one reading of "does this deployment
	// send it" stays contracts.Providers.Active, which is where these carriers
	// land rather than beside it.
	Push push.Adapters

	// Page is the in-app list and the shell it is drawn in, handed in by the
	// composition. Nil mounts no page at all, which is what this module was before
	// it existed: the rows, the two JSON doors and the mail. A capability that
	// guessed which chrome its list sits in would be a capability with a product in
	// it, so the chrome arrives or the screen does not.
	Page *Page
}

// Page is the in-app list as a composition hands it: the page.Shell it is drawn in
// and how many rows one page holds. The screen is this module's and the chrome is
// the composition's, which is why it arrives rather than being built here, and why
// a deployment that provides none has simply no address to ask for.
type Page struct {
	Shell   page.Shell
	PerPage int
}

// Module is the manifest, and the service it is built on: main holds the
// service because the modules that raise notices are wired against it.
//
// permissions is what the manifest declares. It is a var rather than a literal
// in the manifest so that every module answers the question in the same place and
// in the same shape: a reader looking for what a module lets a role be granted
// finds one name, whatever the answer is.
//
// The answer is one key. The two routes this module mounts stay outside it —
// both are about the caller themselves, which is what httpx.SignedIn is for, and
// a permission every signed-in person must hold decides nothing — and it is not
// there for them. It is there because the tenant's sending address is not about
// the caller: the module enforces from inside the transaction that whoever calls
// Put, Verify or Delete is somebody the transaction names, and the route a
// product composes over that face guards it with this key, which kit/app refuses
// to start unless the manifest that defines it says so.
var permissions = []module.Permission{{Key: contracts.PermissionSenderManage, Label: "manage the tenant's sending address"}}

// streams is what a live connection may watch: the two things that happen to one
// person's own notices. The policy reads the payload's own recipient field, and
// that field is the whole of who may watch — a notice is a hint to read, and the
// read is the person's. A connection addressed to anybody else is never told, in
// this tenant or in any other.
//
// The hint carries nothing: a frame says that something happened and when, and the
// list then re-reads its own rows, so what a person sees is what they may read
// rather than what one frame once carried.
var streams = []module.Stream{
	{Event: contracts.EventCreated, Policy: module.StreamRecipient("recipientId")},
	{Event: contracts.EventRead, Policy: module.StreamRecipient("recipientId")},
}

// streamedTopics names what streams watches, in declaration order, for the one
// page that lists these rows. The manifest and the page read this one list, so a
// page cannot watch a topic its own module never declared.
func streamedTopics() []string {
	out := make([]string, 0, len(streams))
	for _, s := range streams {
		out = append(out, s.Event)
	}
	return out
}

// subscriptions is the manifest's list of who does this module's asynchronous
// work: the mail this module sends itself, and one carrier per provider the
// deployment wired, in contracts.Channels' order so the list a reader sees does
// not depend on Go's map iteration. A provider is a channel this deployment
// sends, and a channel it sends has to have somebody listening, so both halves
// — this list and the decision that opens a ledger row — come from
// contracts.Providers.Active over the same Deps value and cannot disagree.
func subscriptions(deps Deps) []events.Subscription {
	subs := []events.Subscription{internal.SendMail(deps.Mailer, deps.Recipients, deps.Hosts, deps.Senders, deps.Secure)}
	for _, c := range deps.Providers.Active() {
		p := deps.Providers.Get(c)
		if c == contracts.ChannelInApp || c == contracts.ChannelEmail {
			// in-app is the row itself and mail is internal.SendMail above, which
			// Module already refuses to compose without. A second carrier for either
			// would be two subscriptions on one event and two sent rows for one
			// delivery, so it fails at the wiring rather than in the worker.
			panic("notification.Module: Deps.Providers carries a channel this module carries itself: " + string(c))
		}
		subs = append(subs, internal.Carrier(p, deps.Hosts, deps.Secure))
	}
	return subs
}

func New(deps Deps) (contracts.Service, module.Module) {
	deps = withDeviceCarriers(deps)
	devices := internal.DeviceStore{Adapters: deps.Push}
	svc := internal.NewService(deps.Recipients,
		internal.WithPreferences(internal.Prefs{}),
		internal.WithSenders(deps.Senders),
		internal.WithProviders(deps.Providers))
	return svc, module.Module{
		Name:       "notification",
		Views:      contracts.Views(deps.Page != nil),
		Migrations: Migrations.Files,
		// What a person was told is their own record; the delivery ledger reaches
		// them only through the notification it delivered. The two preference tables
		// hold no words of the person's, only their id and what they chose, so each
		// names the person it is about and no text.
		PersonalData: personaldata.Implements(
			personaldata.Table{Name: "notifications", Person: "recipient_id", Text: []string{"title", "body", "link"}},
			personaldata.Table{Name: "notification_deliveries", Text: []string{"reason"}, NotPerson: []string{"notification_id"}},
			personaldata.Table{Name: "notification_preferences", About: []string{"recipient_id"}},
			personaldata.Table{Name: "notification_quiet_hours", About: []string{"recipient_id"}},
			// The device list is the person's own, and the token is their device's
			// address: not words somebody wrote about them, so nothing is claimed as
			// text to scrub, and an erasure removes the rows outright. A token that
			// outlived the person it reaches would still deliver to them.
			personaldata.Table{Name: "notification_devices", Person: "recipient_id"},
		),
		Adopts:      Migrations.Adopts,
		Permissions: permissions,
		Declared:    contracts.Events,
		// No nav entry: a nav entry names the permission that decides who sees the
		// link, and the key this module defines guards the tenant's mail identity,
		// not a page. Everybody's notifications are their own, so the link belongs
		// in the chrome the admin shell puts around every page (E4) rather than in
		// the module list.
		Nav: nil,
		// No periodic work: a notification is caused by something happening,
		// which is an event and not the clock (docs/adr/0004).
		Jobs:          nil,
		Subscriptions: subscriptions(deps),
		// The live door onto the two events this module raises for one person,
		// watched by the recipient each payload names. An event not listed here
		// keeps the durable path it has always had; listing one opens a connection
		// onto it and changes nothing about the outbox.
		Streams: streams,
		Routes: func(s httpx.Surfaces) {
			internal.RegisterRoutes(s.App, svc)
			internal.RegisterDeviceRoutes(s.App, devices)
			if deps.Page != nil {
				internal.MountInApp(s, internal.InApp{
					Shell: deps.Page.Shell, Read: svc, PerPage: deps.Page.PerPage,
					Topics: streamedTopics(),
				})
			}
		},
	}
}

// withDeviceCarriers puts a channel in Deps.Providers for every device channel
// these adapters reach, so that the two halves of the wiring — the decision that
// opens a requested row and the subscription that closes it — stay one reading of
// one Deps value. A channel somebody already wired a Provider for keeps theirs:
// the adapters fill the channels nothing else staffs, and never replace them.
func withDeviceCarriers(deps Deps) Deps {
	if len(deps.Push) == 0 {
		return deps
	}
	providers := contracts.Providers{}
	for c, p := range deps.Providers {
		providers[c] = p
	}
	for _, c := range []contracts.Channel{contracts.ChannelPush, contracts.ChannelWebPush} {
		if providers[c] == nil {
			providers[c] = internal.PushCarrier(c, deps.Push)
		}
	}
	deps.Providers = providers
	return deps
}

// Settings is this module's own answer about who may be told what: the channel
// switches, the opt-out and the quiet window, as contracts.PreferenceService and
// contracts.Preferences over the module's own two tables. It needs nothing from
// the composition, because the rows are the module's and every read runs in the
// caller's transaction under the tenant that transaction names.
//
// The screens and routes that reach it are the product's share of this brief:
// every method — Mine's read as much as the three writes — compares the person
// whose id it is handed against the credential the request carries
// (internal.choosing), so the page is a form over one's own rows and there is no
// shape of it that reaches another person's choices.
func Settings() contracts.PreferenceService { return internal.Prefs{} }

// Devices is this module's device table as contracts.DeviceService: the three
// commands a person has over the tokens they registered, over the module's own
// rows, in the caller's transaction. It is the face the module's three device
// routes are mounted on, and a product that reaches the same commands from Go
// builds it the way it builds Settings — with no arguments and no fixture.
//
// Adapters is what registration asks whether a token is carryable, so pass the
// same push.Adapters the composition wired into Deps.Push: a device registered
// against a platform nobody carries is a token that fails every notice to that
// person, and refusing it is the adapter's answer rather than a pattern here.
func Devices(adapters push.Adapters) contracts.DeviceService {
	return internal.DeviceStore{Adapters: adapters}
}

// Senders is the tenant's own sending address as a contracts.Senders and a
// contracts.SenderAdmin: the module keeps the row, the token, the refusal and the
// audit, and the three collaborators below are what it cannot be the authority
// about — whether a domain says so, where the key that signs for it lives, and what
// the caller's roles grant. Any may be nil: a deployment with no verifier cannot
// verify anything, a deployment with no key suppresses the mail it could not sign,
// and a deployment with no grant checker refuses a signed-in caller's sender
// commands rather than guessing they were allowed (contracts.GrantChecker).
func Senders(verifier contracts.SenderVerifier, keys contracts.DKIMKeys, grants contracts.GrantChecker) (contracts.Senders, contracts.SenderAdmin) {
	s := &internal.Senders{Verifier: verifier, Keys: keys, Grants: grants}
	return s, s
}
