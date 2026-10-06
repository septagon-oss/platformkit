package main

// This file is the whole of what this application adds to change control: the two
// subjects it applies proposals for, the two flags it reads, and the two doors
// those flags stand in front of.
//
// modules/change owns the object — the proposal, its digest, its state machine,
// the four-eyes rule — and owns no opinion about which writes need one, because
// that opinion is a fact about an installation. So the facts that are this product's
// own are written here, where a reader can see all of them at once: site settings
// and tasks are the two subjects, `change.control.site-settings` and
// `change.control.task-sla` are the two switches, and each switch bites at the one
// door it was wired in front of. A module that knew any of them would be a module
// with a customer in it.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/flags"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	change "github.com/septagon-oss/platformkit/modules/change"
	changecontracts "github.com/septagon-oss/platformkit/modules/change/contracts"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
	sitecontracts "github.com/septagon-oss/platformkit/modules/site/contracts"
	taskcontracts "github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/ui/page"
)

// proposalAddress is where a write the gate refuses has to go instead. It is the
// change module's own collection as the surface composes it — /api/v1/<module> for
// the app surface, kit/httpx/surfaces.go — written out here because the refusal is
// this product's sentence about its own addresses, and modules/change, which owns
// the door, is the one module that must not name it.
const proposalAddress = "/api/v1/change/proposals"

// siteSettingsFlag is the switch. Off, the settings form writes, which is what it
// did before change control existed. On, that door answers 409 and names
// proposalAddress, and the write goes in as a proposal that an account other than
// the proposer applies. The key is this application's: another product that gates
// a different write picks its own key and its own subject.
const siteSettingsFlag = "change.control.site-settings"

// The two subjects are contributed one per module — product contributes the task and
// access the site settings — because a contribution is the one value of a contract a
// module *is*, and the resolver counts them: `Use(change.Module)` gets both.
// site.Module is composed over settingsGate. The proposals answer whether or not
// either flag is on: a switch decides which door a write comes through, not whether
// the object exists — with both switches off a person can still get a second pair of
// eyes by proposing a change. Everything here is typed by the contracts it satisfies,
// so the compiler checks the graph and no reader has to run a generator.

// configFlags is kit/flags' Evaluator over what kit/config read out of the flags
// block: one installation, one process, one boolean per key, and the subject a
// caller passes is accepted and unused — there is no targeting here, not even by
// tenant. An installation that named no flag at all is an empty map: "off".
//
// It is here rather than in kit/flags/providers because it is not a provider worth
// installing: there is no SDK, no wire format and no service behind it, and a
// package for eight lines of map lookup is a package that invites somebody to put
// real targeting in it later. The contract it satisfies is kit/flags', including
// the half that matters — a key that is not configured answers the caller's
// fallback with Defaulted set, which is how "this installation has never heard of
// that flag" stays distinguishable from "it is off".
type configFlags map[string]bool

var _ flags.Evaluator = configFlags(nil)

func (f configFlags) Boolean(_ context.Context, key string, _ flags.Subject, fallback bool) (flags.Decision, error) {
	value, configured := f[key]
	if !configured {
		return flags.Decision{Value: fallback, Defaulted: true}, nil
	}
	return flags.Decision{Value: value}, nil
}

// settingsGate answers modules/site's question — may this tenant's settings be
// written directly? — by asking the flag. It is the only place in this application
// where a flag changes what a request gets, and it refuses rather than silently
// dropping the write: the answer has to say where the write goes.
type settingsGate struct{ eval flags.Evaluator }

var _ sitecontracts.WriteGate = settingsGate{}

// flagUnreadable is the door's answer when the switch could not be read at all.
//
// kit/flags' rule for a failed evaluation is to hand the caller the fallback it
// passed — false here, meaning "change control is off, write away" — so a provider
// that is unreachable, unauthorised or misconfigured would read as a switch that is
// off, and the settings would change hands with no second account in sight. That
// answer is a silent allow (decision 0010) and, worse, one reachable by anybody who
// can make the provider fail.
//
// Refusing takes nothing from anybody: the proposal door reads no flag, so the
// write can still be put forward and decided by somebody else while the provider is
// down. What the caller gets is the status that says try again and the address that
// works now; what it does not get is the provider's own error, because kit/problem
// keeps a 5xx's cause on the server's side of that line.
//
// It builds a fresh Problem per call rather than being one Problem at package scope,
// because kit/httpx's response transformer writes the request id into the Problem a
// handler returns (stampRequestID, kit/httpx/request_id.go:94): one shared value
// would be two requests writing one field at once, and the second body would carry
// the first request's id.
func flagUnreadable() error {
	return problem.New(http.StatusServiceUnavailable,
		"CHANGE_CONTROL_UNAVAILABLE: "+siteSettingsFlag+" did not answer, so this write is refused; "+
			proposalAddress+" is decided by a second account whether or not that switch is reachable")
}

func (g settingsGate) Check(ctx context.Context, tx db.Tx[db.Tenant]) error {
	tenant := db.TenantOf(tx)
	actor, _ := tenancy.ActorFrom(ctx)
	decision, err := g.eval.Boolean(ctx, siteSettingsFlag,
		flags.Subject{TenantID: tenant.ID, TargetingKey: actor.String()}, false)
	if err != nil {
		// An unreadable switch is not a switch that is off. The fallback belongs to
		// the answer a provider *gives*, not to the one it fails to give, and the
		// door refuses rather than borrowing "off" from an outage (flagUnreadable).
		return flagUnreadable()
	}
	if !decision.Value {
		return nil
	}
	// crud.ErrConflict is the status — the row is not in a state this door writes —
	// and the wrapped Refusal is the sentence: a refusal that does not name the way
	// through it is not an answer (changecontracts.Refusal).
	return fmt.Errorf("%w: %w", crud.ErrConflict, &changecontracts.Refusal{
		SubjectModule: "site", SubjectEntity: "settings", SubjectID: uuid.Nil,
		Path: proposalAddress, Permission: changecontracts.PermissionChangePropose,
	})
}

// siteSubject is the tenant's site settings as a change subject: the two methods
// modules/change needs to know what a row looks like and to write it, in the hands
// of the module that owns the row. Nothing here opens site_settings — the site
// service reads and writes its own table, and this is the adapter that says which
// of its doors a proposal comes through.
type siteSubject struct {
	sites  sitecontracts.Service
	locked sitecontracts.LockedReader
}

var _ changecontracts.Subject = siteSubject{}

// Lock is the read a diff is made against, taken with the row locked, so the
// revision recorded beside the diff is the revision nobody else can move while
// this transaction is open. A tenant that has never saved its site is at revision
// 0 with the defaults, which is a real base revision: the first write to this
// tenant's settings is as reviewable as the tenth.
func (s siteSubject) Lock(ctx context.Context, tx db.Tx[db.Tenant]) (json.RawMessage, int64, error) {
	current, err := s.locked.SettingsForUpdate(ctx, tx)
	if err != nil {
		return nil, 0, err
	}
	encoded, err := json.Marshal(current)
	if err != nil {
		return nil, 0, fmt.Errorf("site: the settings a diff was made against cannot be written down: %w", err)
	}
	return encoded, current.Revision, nil
}

// Save writes the merged document through the site service, which validates it as
// a site and publishes site.settings_updated beside the proposal's own event. It
// returns the revision the row is on now, which is what the proposal records as
// applied.
//
// This is Service.Save and not the settings route: the gate stands on the route,
// so an approved proposal is never refused by the flag that sent it through the
// proposal in the first place.
func (s siteSubject) Save(ctx context.Context, tx db.Tx[db.Tenant], merged json.RawMessage) (int64, error) {
	var next sitecontracts.SiteSettings
	if err := json.Unmarshal(merged, &next); err != nil {
		return 0, fmt.Errorf("%w: the merged document is not a site settings document: %v", crud.ErrInvalid, err)
	}
	saved, err := s.sites.Save(ctx, tx, &next)
	if err != nil {
		return 0, err
	}
	return saved.Revision, nil
}

// siteSubjectBinding is the tenant's site settings as one change subject, named by its
// module and entity the way the manifest's Nav entries name their screens. This
// product contributes two subjects, one from each of the two modules that hold a
// service able to read one, and a subject no module contributes does not exist —
// which is contracts.ErrUnsupportedSubject's whole meaning.
func siteSubjectBinding(sites sitecontracts.Service, locked sitecontracts.LockedReader) changecontracts.SubjectBinding {
	return changecontracts.SubjectBinding{
		Module: "site", Entity: "settings",
		Resolve: func(_ context.Context, _ db.Tx[db.Tenant], subjectID uuid.UUID) (changecontracts.Subject, error) {
			// Site settings are one row per tenant, which is what the nil uuid
			// means in contracts.Proposal. A proposal that named a settings row
			// by id was written by somebody who guessed, and the guess is
			// refused rather than resolved to the one row there is.
			if subjectID != uuid.Nil {
				return nil, fmt.Errorf("%w: a tenant has one site, and %s names no second one",
					crud.ErrInvalid, subjectID)
			}
			return siteSubject{sites: sites, locked: locked}, nil
		},
	}
}

// The task is this product's second change subject, and the reason change control
// exists as a kernel object rather than a pattern: nothing in modules/change
// changed to admit it. Two things did — a `revision` column on the row
// (modules/task/migrations/000050) so a diff has a number to quote, and the door in
// front of the five routes kit/rest mounts for it (task.Deps.Gate). The three facts
// below are this product's own: which switch says whether a task edit needs a second
// pair of eyes, which of a task's fields can ever be moved by a proposal, and which
// row a proposal is about.

// taskSLAFlag is the second switch. Off — which is what config.example.yaml ships,
// and what an installation with no flags block gets — the task screen writes a
// deadline as it always did. On, a PATCH of a protected field answers 409 naming the
// field and proposalAddress, and the change goes in as a proposal that an account
// other than the proposer applies. The key is this application's, one boolean for
// the installation rather than one per tenant, which is the shape configFlags is.
const taskSLAFlag = "change.control.task-sla"

// taskProtection answers "of the fields this write moves, which may not be written
// without a proposal" for a task, and for nothing else: it is the intersection of
// the module's published vocabulary with this product's opinion about which of those
// fields carry a promise. slaDeadline is the contractual deadline and priority is the
// urgency in it; a title is not worth four eyes and this installation says so here,
// in the file that names every product fact, rather than in the module that owns the
// row.
//
// The second half of the answer is the wall/door rule: a field whose owner is the
// command being run is never protected at that command's own door. With the switch
// on, Resolve still resolves — refusing it would leave a tenant unable to close a
// task while a flag was set, which is a wall with a flag on it.
type taskProtection struct{ eval flags.Evaluator }

var _ changecontracts.Protection = taskProtection{}

func (p taskProtection) Protected(ctx context.Context, tx db.Tx[db.Tenant], w changecontracts.DirectWrite) ([]string, error) {
	if w.Command != "" {
		// A command's own fields are its own. kit/rest asks about every command a
		// gated Spec mounts — it cannot know what a command writes without running
		// it, so it asks by verb and lets this answer say — and this is where it
		// says: Assign, Resolve and CheckSLA each move a state and publish an event
		// beside it, and an installation that protects a task's deadline still has
		// to be able to close a task. Naming a protected field here would be a wall
		// with a flag on it, because the proposal door writes no command: an apply
		// goes through taskcontracts.Writer, never through Resolve.
		return nil, nil
	}
	want := map[string]bool{ // this product's opinion, over the module's vocabulary
		"slaDeadline": true,
		"priority":    true,
	}
	var out []string
	for _, field := range w.Changed {
		if !want[field] || !slices.Contains(taskcontracts.ProtectableFields, field) {
			continue
		}
		out = append(out, field)
	}
	return out, nil
}

// taskSubject is one task as a change subject: the two methods modules/change needs
// to know what the row looks like and to write it, in the hands of the module that
// owns the table. Nothing here opens `tasks`.
type taskSubject struct {
	tasks  taskcontracts.Writer
	locked taskcontracts.LockedReader
	// id is the row this proposal is about, bound once per command by the
	// Resolve below, so neither method asks which task it was handed and a merged
	// document cannot name a different one.
	id uuid.UUID
}

var _ changecontracts.Subject = taskSubject{}

// Lock is the read a diff is made against, with the row locked, so the revision
// beside the document is the revision nobody else can move while this transaction is
// open. A task that does not exist in this tenant answers crud.ErrNotFound from the
// read itself, and RLS is what answered it.
func (s taskSubject) Lock(ctx context.Context, tx db.Tx[db.Tenant]) (json.RawMessage, int64, error) {
	current, err := s.locked.TaskForUpdate(ctx, tx, s.id)
	if err != nil {
		return nil, 0, err
	}
	encoded, err := json.Marshal(current)
	if err != nil {
		return nil, 0, fmt.Errorf("task: the row a diff was made against cannot be written down: %w", err)
	}
	return encoded, current.Revision, nil
}

// Save writes the merged document through the task module, which validates it as a
// task, refuses one that moves a field a command owns, moves the revision, and
// publishes task.task.updated beside the proposal's own change.proposal_applied — so
// the apply leaves one trail with two rows in it and one actor, not two accounts of
// the same write.
func (s taskSubject) Save(ctx context.Context, tx db.Tx[db.Tenant], merged json.RawMessage) (int64, error) {
	var next taskcontracts.Task
	if err := json.Unmarshal(merged, &next); err != nil {
		return 0, fmt.Errorf("%w: the merged document is not a task: %v", crud.ErrInvalid, err)
	}
	next.ID = s.id // the proposal's subject is the row; the document cannot choose it
	saved, err := s.tasks.Save(ctx, tx, &next)
	if err != nil {
		return 0, err
	}
	return saved.Revision, nil
}

// taskSubjectBinding is this product's second change subject, contributed by the
// product module: a task always has an id, so a proposal that named uuid.Nil was
// written by somebody who guessed, and the guess is refused rather than resolved to
// whichever row they happened to be able to read.
func taskSubjectBinding(tasks taskcontracts.Writer, locked taskcontracts.LockedReader) changecontracts.SubjectBinding {
	return changecontracts.SubjectBinding{
		Module: "task", Entity: "task",
		Resolve: func(_ context.Context, _ db.Tx[db.Tenant], subjectID uuid.UUID) (changecontracts.Subject, error) {
			if subjectID == uuid.Nil {
				return nil, fmt.Errorf("%w: a task always has an id, and %s names none",
					crud.ErrInvalid, subjectID)
			}
			return taskSubject{tasks: tasks, locked: locked, id: subjectID}, nil
		},
	}
}

// taskGate builds the door in front of the five routes kit/rest mounts for a task:
// the module's field vocabulary, this product's opinion about which of them carries a
// promise, and the switch that says whether any of it is on today. modules/change
// answers with the refusal; kit/rest asks; modules/task hands the answer to its own
// mount, because a Spec mounts its own routes and a door has to be handed in rather
// than written around them.
func taskGate(eval flags.Evaluator) rest.Gate {
	return change.Door(change.NewGate(change.Gate{
		Flags:      eval,
		Key:        taskSLAFlag,
		WayOn:      proposalAddress,
		Subject:    changecontracts.SubjectRef{Module: "task", Entity: "task"},
		Protection: taskProtection{},
	}))
}

// WritableFields is the answer that makes a task proposal a promise: the fields an
// apply can move are exactly the ones Writer.Save writes, which is the module's own
// published vocabulary and not a second list this file could drift from.
//
// It is the propose door's question — "could any apply ever carry this out as reviewed"
// — and a diff naming the status a command owns, the revision the server owns, or a
// column that is not a field of a task at all is refused there rather than digested,
// approved, applied, and found not to have happened.
func (taskSubject) WritableFields() []string { return taskcontracts.ProtectableFields }

// proposalNotifier is modules/change's notice port over this application's notification
// service: four lines, because the module already knows who to tell, what to call it and
// where the page is. What this file adds is the sentence — which is this product's copy,
// the same reason the flag keys and the proposal address are here and not in the module.
//
// Notify writes the row, publishes notification.created and asks for mail in the same
// transaction the decision's event was claimed in, so "notification owns delivery records"
// stays true and nothing in change knows a mail server exists.
type proposalNotifier struct{ notices notificationcontracts.Service }

var _ changecontracts.Notifier = proposalNotifier{}

func (n proposalNotifier) Told(ctx context.Context, tx db.Tx[db.Tenant], p changecontracts.Notice) error {
	title, body := verdictSaid(p.Kind, p.State)
	_, err := n.notices.Notify(ctx, tx, notificationcontracts.Notice{
		Recipient: p.Recipient, Title: title, Body: body, Link: p.Link, Email: true,
	})
	return err
}

// verdictSaid is the one line a proposer is told, in this product's words, from the two
// facts the module handed over. The diff is nowhere in it, and cannot be: the Notice the
// module builds carries no diff to put here, which is what keeps a value under review out
// of notifications.link and notifications.body forever.
func verdictSaid(kind, state string) (title, body string) {
	switch {
	case kind == changecontracts.NoticeApplied:
		return "Your proposed change was applied",
			"The change you proposed has been applied to the record it was made against."
	case state == changecontracts.StateApproved:
		return "Your proposed change was approved",
			"An account other than yours approved it; it is waiting to be applied."
	case state == changecontracts.StateDeclined:
		return "Your proposed change was declined",
			"An account other than yours declined it. Nothing changed."
	default:
		return "Your proposed change was decided",
			"It is now " + state + "."
	}
}

// pinnedProposals is the queue's address, as the surface composes it: /app plus the
// module namespace plus the page modules/change mounts. Quoted here for the same reason
// the other pinned addresses are — a notice and a refusal both name it, and only the
// running server says whether the composition really put it there.
const pinnedProposals = "/app/change/proposals"

// reviewShell is the chrome the two review pages are drawn in: the composition's own
// brand, stylesheet, way back and merged catalogue, which are the same four facts the
// fault pages use. modules/change owns the two pages and no opinion about the chrome
// around them (see ui.Pages), so this is the line where the application supplies one.
func reviewShell(messages page.Messages) page.Shell {
	return page.Shell{
		Chrome:    faultChrome(),
		Frame:     faultFrame,
		Tag:       "change",
		Back:      pinnedHome,
		BackLabel: "Back to the workspace",
		Messages:  messages,
	}
}
