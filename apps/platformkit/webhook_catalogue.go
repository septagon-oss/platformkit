package main

// webhook_catalogue.go is this composition's answer to the one question
// modules/webhook cannot ask itself: which events can *this* application emit? An
// endpoint's event list is only checkable against what the composition can deliver,
// and no module that is not built last can enumerate that — the emitting modules are
// composed by this file, so this file writes the answer. It is a literal, beside the
// literal `catalogues()` writes, for the same reason: an installation that ships
// words for a language writes them out, and one that emits events names them.
//
// The names are the emitting modules' own constants, so a rename moves the list with
// the compiler. What that cannot catch is a whole module's worth of names missing,
// which is what TestTheWebhookCatalogueNamesWhatTheCompositionEmits asks the resolved
// composition rather than this file. A stale entry is an option a screen offers that
// never fires, because delivery never consults the catalogue.

import (
	"github.com/septagon-oss/platformkit/kit/httpx"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	billingcontracts "github.com/septagon-oss/platformkit/modules/billing/contracts"
	changecontracts "github.com/septagon-oss/platformkit/modules/change/contracts"
	contentcontracts "github.com/septagon-oss/platformkit/modules/content/contracts"
	filecontracts "github.com/septagon-oss/platformkit/modules/file/contracts"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
	savedcontracts "github.com/septagon-oss/platformkit/modules/saved/contracts"
	scimcontracts "github.com/septagon-oss/platformkit/modules/scim/contracts"
	sitecontracts "github.com/septagon-oss/platformkit/modules/site/contracts"
	subjectcontracts "github.com/septagon-oss/platformkit/modules/subject/contracts"
	taskcontracts "github.com/septagon-oss/platformkit/modules/task/contracts"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	translationcontracts "github.com/septagon-oss/platformkit/modules/translation/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	webhookcontracts "github.com/septagon-oss/platformkit/modules/webhook/contracts"
)

// webhookCatalogue is the whole list, by the module that emits each name.
var webhookCatalogue = emittedEvents{
	// A person's own copies: the two names the saving service emits, each in the
	// transaction that writes or withdraws the copy. A product that composes the
	// capability and not these two would emit events no subscriber could be told
	// about.
	savedcontracts.EventSaved, savedcontracts.EventUnsaved,
	// auth, and the two names its registration add-on emits.
	authcontracts.EventLoggedIn, authcontracts.EventLoggedOut, authcontracts.EventLoginFailed,
	authcontracts.EventResetRequested, authcontracts.EventPasswordReset,
	authcontracts.EventRoleSet, authcontracts.EventSessionRevoked,
	authcontracts.EventFactorEnrolled, authcontracts.EventFactorWithdrawn,
	authcontracts.EventRecoveryCodesIssued, authcontracts.EventRecoveryCodeUsed,
	authcontracts.EventAPITokenIssued, authcontracts.EventAPITokenRevoked,
	authcontracts.EventVerificationRequested, authcontracts.EventRegistrationRequested,
	authcontracts.EventFactorUsed, authcontracts.EventFactorSuspect,
	authcontracts.EventPasskeySignInSet,
	// The two names the mail delivery record added: a sign-up that sent no link,
	// and a mail this module could not hand to a transport. Both are emitted in the
	// transaction that writes the delivery record, so a subscriber that asked for the
	// record's trail would otherwise be refused the two events that say the record
	// exists.
	authcontracts.EventNoLinkRequested, authcontracts.EventMailFailed,
	// The refusal of a role write that would have left the tenant with nobody who
	// can administer it. The refused write rolled back, so this event is the only
	// record that the attempt was made, and an installation that watches its own
	// administration floor subscribes to nothing else.
	authcontracts.EventAdministrationRefused,
	// user, including the doors registration opened.
	usercontracts.EventCreated, usercontracts.EventUpdated, usercontracts.EventDeleted,
	usercontracts.EventInvited, usercontracts.EventPasswordSet, usercontracts.EventRolesSet,
	usercontracts.EventHandleSet, usercontracts.EventDeactivated,
	usercontracts.EventAdministrationRefused,
	usercontracts.EventRegistrationPending, usercontracts.EventRegistrationApproved,
	usercontracts.EventRegistrationUnverified, usercontracts.EventEmailVerified,
	// The return an administrator's own command drives: a suspended person is an
	// account that cannot sign in, and a subscriber watching who may enter would
	// otherwise hear the door close and never hear it open again.
	usercontracts.EventReactivated,
	// scim, the five facts no user event describes: the collection declared with
	// nobody in it, the collection renamed, the collection withdrawn, this tenant's
	// binding of a collection to a role, and the attribute of a person this door
	// writes and modules/user publishes nothing about. The person's own invitation,
	// roles and deactivation stay the `user.` names above.
	scimcontracts.EventGroupCreated, scimcontracts.EventGroupChanged, scimcontracts.EventGroupRemoved,
	scimcontracts.EventMappingChanged, scimcontracts.EventUserChanged,
	// tenant, whose lifecycle an installation's own operator acts drive.
	tenantcontracts.EventCreated, tenantcontracts.EventSuspended, tenantcontracts.EventHostAdded,
	tenantcontracts.EventHostRemoved, tenantcontracts.EventLocaleSet, tenantcontracts.EventRenamed,
	tenantcontracts.EventReactivated, tenantcontracts.EventDeleted,
	tenantcontracts.EventLifecycleRecorded, tenantcontracts.EventOIDCSet, tenantcontracts.EventOIDCCleared,
	tenantcontracts.EventSAMLSet, tenantcontracts.EventSAMLCleared,
	// notification, task, content, site, file, billing, change.
	notificationcontracts.EventCreated, notificationcontracts.EventEmailRequested, notificationcontracts.EventRead,
	// The channels the tenant's sender and the person's own switches opened up. One
	// name per channel rather than one event carrying a channel field, so an endpoint
	// that answers 500 all afternoon delays its own channel and nothing else; a
	// subscriber that watches what a notice was asked to do names the channel it cares
	// about and no other.
	notificationcontracts.EventPushRequested, notificationcontracts.EventWebPushRequested,
	notificationcontracts.EventWebhookRequested,
	// The three moments of a person's and a tenant's own decisions: which switches are
	// on, which address this tenant sends from, and whether that address ever proved
	// itself. A removal of a sender that was never believed is its own name rather than
	// a sender_set saying "pending", and an endpoint that watches the tenant's mail
	// identity wants the same distinction — see modules/notification/contracts/events.go.
	notificationcontracts.EventPreferenceSet, notificationcontracts.EventSenderSet,
	notificationcontracts.EventSenderVerified, notificationcontracts.EventSenderRemoved,
	// Where a person can be reached, and where that stopped being true. A registration
	// is the holder's own act, so the event is the only way an installation watching its
	// own delivery floor learns the reachability changed; a removal is the provider's
	// answer rather than anybody's command, and its payload names no actor for that
	// reason — see modules/notification/internal/carrier_push.go.
	notificationcontracts.EventDeviceRegistered, notificationcontracts.EventDeviceRemoved,
	taskcontracts.EventCreated, taskcontracts.EventUpdated, taskcontracts.EventDeleted,
	taskcontracts.EventAssigned, taskcontracts.EventResolved, taskcontracts.EventSLABreached,
	contentcontracts.EventCreated, contentcontracts.EventUpdated, contentcontracts.EventDeleted,
	contentcontracts.EventPublished, contentcontracts.EventUnpublished, contentcontracts.EventArchived,
	sitecontracts.EventSettingsUpdated,
	// history, whose one event says a stored version of a record came back. The
	// name rides on the kit's Versions port and not on a module's contracts, for
	// the reason that port gives: the kit publishes it for whichever resource asked
	// for history, and modules/history declares it typed in its manifest.
	httpx.RestoredEvent,
	// translation, whose one event says a record's other languages moved.
	translationcontracts.EventUpdated,
	// subject, the two moments of a person's own request and the erasure itself. An
	// installation that serves a person's export has to be able to tell whoever holds
	// the link that the bundle exists, and a tenant that erased a person wants the
	// fact pushed rather than polled: all three names are emitted in the transaction
	// that writes the row they describe, so a subscriber that watches the queue sees
	// the queue move.
	subjectcontracts.EventExportRequested, subjectcontracts.EventExportReady,
	subjectcontracts.EventErased,
	filecontracts.EventUploaded, filecontracts.EventDeleted, filecontracts.EventRetained,
	filecontracts.EventReleased, filecontracts.EventErased,
	// The flip the image widths added: a file's audience is recomputed whenever the
	// records that show it change, so an image is public while something shows it and
	// private again when the last of them stops. A subscriber that watches what an
	// installation exposes needs that moment, not only the upload.
	filecontracts.EventVisibility,
	// What a picture says about itself, and the claim that it says nothing. One
	// command writes both and publishes in its own transaction, so a subscriber that
	// watches whether an installation's images are described watches this name: the
	// upload says a file arrived, never that anyone captioned it.
	filecontracts.EventAltered,
	billingcontracts.EventPlanCreated, billingcontracts.EventPlanUpdated, billingcontracts.EventPlanDeleted,
	billingcontracts.EventSubscribed, billingcontracts.EventCancelled, billingcontracts.EventRenewed,
	billingcontracts.EventPastDue,
	changecontracts.EventProposed, changecontracts.EventReviewed,
	changecontracts.EventApplied, changecontracts.EventWithdrawn,
	// webhook's own: a subscription that watches subscriptions.
	webhookcontracts.EventEndpointCreated, webhookcontracts.EventEndpointUpdated,
	webhookcontracts.EventEndpointDeleted, webhookcontracts.EventEndpointDisabled,
	webhookcontracts.EventEndpointEnabled,
}

// emittedEvents is the port as this application answers it: the list, and nothing
// else. The module copies it once per write.
type emittedEvents []string

func (e emittedEvents) EventTypes() []string { return e }
