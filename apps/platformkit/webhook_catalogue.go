package main

// webhook_catalogue.go is this composition's answer to the one question
// modules/webhook cannot ask itself: which events can *this* application emit?
//
// An endpoint's event list is only checkable against what the composition can
// actually deliver, and no module that is not built last can enumerate that — the
// modules that emit are composed by this file, so this file writes the answer. It
// is a literal, beside the literal `catalogues()` writes for the same reason: an
// installation that ships words for a language writes them out, and an installation
// that emits events names them.
//
// The names are the emitting modules' own constants rather than strings typed by
// hand, so a rename moves the list with the compiler and not through a reviewer.
// What that cannot catch is a *module* — a whole module's worth of names missing
// from the list — and that is what
// TestTheWebhookCatalogueNamesWhatTheCompositionEmits asks the resolved composition
// rather than this file. A stale entry is not a broken delivery: it is an option a
// screen offers that never fires, because delivery never consults the catalogue. It
// never refuses a socket on the strength of a name having left the list.

import (
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	billingcontracts "github.com/septagon-oss/platformkit/modules/billing/contracts"
	changecontracts "github.com/septagon-oss/platformkit/modules/change/contracts"
	contentcontracts "github.com/septagon-oss/platformkit/modules/content/contracts"
	filecontracts "github.com/septagon-oss/platformkit/modules/file/contracts"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
	sitecontracts "github.com/septagon-oss/platformkit/modules/site/contracts"
	taskcontracts "github.com/septagon-oss/platformkit/modules/task/contracts"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	webhookcontracts "github.com/septagon-oss/platformkit/modules/webhook/contracts"
)

// webhookCatalogue is the whole list, by the module that emits each name.
var webhookCatalogue = emittedEvents{
	// auth, and the two names its registration add-on emits.
	authcontracts.EventLoggedIn, authcontracts.EventLoggedOut, authcontracts.EventLoginFailed,
	authcontracts.EventResetRequested, authcontracts.EventPasswordReset,
	authcontracts.EventRoleSet, authcontracts.EventSessionRevoked,
	authcontracts.EventFactorEnrolled, authcontracts.EventFactorWithdrawn,
	authcontracts.EventRecoveryCodesIssued, authcontracts.EventRecoveryCodeUsed,
	authcontracts.EventAPITokenIssued, authcontracts.EventAPITokenRevoked,
	authcontracts.EventVerificationRequested, authcontracts.EventRegistrationRequested,
	// user, including the doors registration opened.
	usercontracts.EventCreated, usercontracts.EventUpdated, usercontracts.EventDeleted,
	usercontracts.EventInvited, usercontracts.EventPasswordSet, usercontracts.EventRolesSet,
	usercontracts.EventHandleSet, usercontracts.EventDeactivated,
	usercontracts.EventRegistrationPending, usercontracts.EventRegistrationApproved,
	usercontracts.EventRegistrationUnverified, usercontracts.EventEmailVerified,
	// tenant, whose lifecycle an installation's own operator acts drive.
	tenantcontracts.EventCreated, tenantcontracts.EventSuspended, tenantcontracts.EventHostAdded,
	tenantcontracts.EventHostRemoved, tenantcontracts.EventLocaleSet, tenantcontracts.EventRenamed,
	tenantcontracts.EventReactivated, tenantcontracts.EventDeleted,
	tenantcontracts.EventLifecycleRecorded, tenantcontracts.EventOIDCSet, tenantcontracts.EventOIDCCleared,
	// notification, task, content, site, file, billing, change.
	notificationcontracts.EventCreated, notificationcontracts.EventEmailRequested, notificationcontracts.EventRead,
	taskcontracts.EventCreated, taskcontracts.EventUpdated, taskcontracts.EventDeleted,
	taskcontracts.EventAssigned, taskcontracts.EventResolved, taskcontracts.EventSLABreached,
	contentcontracts.EventCreated, contentcontracts.EventUpdated, contentcontracts.EventDeleted,
	contentcontracts.EventPublished, contentcontracts.EventUnpublished, contentcontracts.EventArchived,
	sitecontracts.EventSettingsUpdated,
	filecontracts.EventUploaded, filecontracts.EventDeleted, filecontracts.EventRetained,
	filecontracts.EventReleased, filecontracts.EventErased,
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
