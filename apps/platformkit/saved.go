package main

// saved.go is this application's share of a person keeping a copy of a record: the
// authority read the saving service asks when it is handed no caller, the one pair
// this product says may be kept, the source read that copy is captured from, and
// the three addresses a generated control posts to.
//
// The capability — a private copy that outlives its source, its idempotent save, its
// revision check, its events — is modules/saved's, and nothing is restated here.
// What is not any module's fact is: who is asking (this application's own membership
// and grant tables, read through their owners), and which of this product's records
// are worth keeping a copy of. A product that composes the module and writes none of
// this gets a service with nothing to capture and no control on any screen, which is
// the absence Explain names rather than leaves silent.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	savedcontracts "github.com/septagon-oss/platformkit/modules/saved/contracts"
	taskcontracts "github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/pkit"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/resource"
	uisaved "github.com/septagon-oss/platformkit/ui/saved"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// saving answers the two questions the saving service cannot answer for itself:
// whose copy this is, and what may be kept. It is composed immediately before the
// module that asks them, for the reason library.go gives for the media chrome beside
// modules/file: the module builds its service out of what is on the table by then,
// and a pair contributed after it would be a declaration the service never saw.
//
// Its Needs stop at auth and the task desk's own read. The service it hands to a
// person's screen is provided by the module below it, so a module that needed it
// would be the cycle pkit refuses (`saving → saved → saving`); the page port is
// therefore filled by keeping(), which is composed after the service exists.
func saving() *pkit.Module {
	return pkit.NewModule("saving", func(w *pkit.Wiring) (module.Module, error) {
		auth := pkit.Get[authcontracts.Auth](w)
		tasks := pkit.Get[taskcontracts.Reader](w)
		pkit.Put[savedcontracts.Access](w, productAccess{auth: auth})
		pkit.Put[savedcontracts.Clock](w, func() time.Time { return db.Now() })
		pkit.Put[savedcontracts.Savable](w, savedcontracts.Savable{
			Module: "task", Kind: "task",
			Words: savedcontracts.Words{
				Save: "Save task", Saved: "Saved to your tasks",
				Remove: "Remove from saved", ListTitle: "My saved tasks",
			},
			Source: taskSource{tasks: tasks},
		})
		return module.Module{Name: "saving"}, nil
	},
		pkit.Needs[authcontracts.Auth](),
		pkit.Needs[taskcontracts.Reader](),
		pkit.Provides[savedcontracts.Access](),
		pkit.Provides[savedcontracts.Clock](),
		pkit.Contributes[savedcontracts.Savable](),
	)
}

// keptSurface is this composition's answer to the one question the served document
// and the generated screens cannot ask each other: did this installation compose the
// saving capability at all. It is read off the resolved plan beside the seed
// provisioner and the roles catalogue, for the reason app.go gives for those: the
// sentence is written before anything is resolved, and only the plan knows which
// modules it built. A product that took the capability out of its sentence therefore
// publishes no `savable` and draws no control, rather than a promise nothing honours.
type keptSurface struct{ saving bool }

// keepsCopies answers that question from what the composition actually built. The
// two grants a kept copy is read and written under are declared by the module that
// owns the capability and by nothing else, so their presence in the resolved
// manifests is the composition's own statement that keeping copies exists here — and
// the one statement that survives somebody editing the sentence below.
func keepsCopies(mods []module.Module) bool {
	for _, grant := range module.Grants(mods) {
		if grant.Permission == savedcontracts.PermissionWrite {
			return true
		}
	}
	return false
}

// installed is that answer in the form ui/screens reads it.
func (k *keptSurface) installed() screens.Installed {
	return screens.Installed{Saving: k != nil && k.saving}
}

// keeping is the same capability seen from a person's screen: the standing a record
// draws its control from, the grid of what they kept, and the two commands that keep
// one and give one back. It needs the service, so it is composed after the module
// that provides it, and it mounts the three routes in the chrome this application
// mounts its own pages in.
//
// The two ports are one adapter, because "have I kept this row", "what have I kept",
// "keep this" and "give that back" are one service seen from four sides — and two
// adapters would be two places for the mounted addresses to disagree.
func keeping(shell *page.Shell, surface *keptSurface) *pkit.Module {
	return pkit.NewModule("keeping", func(w *pkit.Wiring) (module.Module, error) {
		// This module is the capability, so its being built is the answer: a sentence
		// that took the capability out never runs this line, and the document and the
		// record screen then answer "nothing installed" instead of promising a
		// control no composition wrote.
		surface.saving = true
		desk := &keepingDoor{
			service: pkit.Get[savedcontracts.Service](w),
			access:  productAccess{auth: pkit.Get[authcontracts.Auth](w)},
		}
		pkit.Put[resource.SavingReader](w, desk)
		pkit.Put[uisaved.Keeping](w, desk)
		return module.Module{
			Name: "keeping",
			Routes: func(s httpx.Surfaces) {
				// The addresses come back from the mount that made them and are
				// written into the adapter before the first request is served, which
				// is the order the kernel works in. A control that posts to a guessed
				// address is the failure this repository has already paid for once;
				// here the page that owns the routes says where they answer and the
				// renderer derives nothing. Which surface a person's pages stand on is
				// this composition's decision and not the saving module's, which
				// declares no routes at all.
				desk.at = uisaved.Mount(s.App, *shell, desk, uisaved.Options{Workspace: pinnedWorkspace})
			},
		}, nil
	},
		pkit.Needs[savedcontracts.Service](),
		pkit.Needs[authcontracts.Auth](),
		pkit.Provides[resource.SavingReader](),
		pkit.Provides[uisaved.Keeping](),
	)
}

// keepingDoor is that one adapter. Its mounted addresses arrive from the mount
// above and are empty until then, which is the order the kernel works in: routes are
// mounted before the first request is served.
type keepingDoor struct {
	service savedcontracts.Service
	access  productAccess
	at      uisaved.At
}

// productAccess is the saving service's authority question, answered out of this
// application's own membership and grants.
//
// It is asked inside the caller's transaction and not from the principal the request
// carries, because a Save may have waited on the source's row before asking: the
// person who was a coordinator when the request opened and whose role was taken while
// it waited is not one. The bearer ceiling is then applied to whatever the roles
// granted, because a scoped key narrows a holder's authority and never adds to it.
type productAccess struct{ auth authcontracts.Auth }

func (a productAccess) Current(ctx context.Context, tx db.Tx[db.Tenant], permission string) (string, error) {
	if permission != savedcontracts.PermissionRead && permission != savedcontracts.PermissionWrite {
		// Unreachable from any route: a guard names a permission its own module
		// declared. Refused rather than answered, because an unknown grant string
		// asked of this read would otherwise be granted by whoever wrote it.
		return "", savedcontracts.ErrInvalid
	}
	principal, ok := tenancy.PrincipalFrom(ctx)
	if !ok || principal.UserID == uuid.Nil {
		return "", savedcontracts.ErrDenied
	}
	granted, err := a.auth.Permissions(ctx, tx, principal.Roles)
	if err != nil {
		return "", err
	}
	if !heldBy(granted, permission) {
		return "", savedcontracts.ErrDenied
	}
	// A bearer credential carries its own list, and it is a ceiling: an empty one
	// authorises nothing whatever the holder's roles say.
	if principal.Permissions != nil && !heldBy(principal.Permissions, permission) {
		return "", savedcontracts.ErrDenied
	}
	return principal.UserID.String(), nil
}

// heldBy is the grant test both halves ask, written once because the two answers
// must be one test rather than two that happen to agree.
func heldBy(granted []string, permission string) bool {
	for _, g := range granted {
		if g == permission {
			return true
		}
	}
	return false
}

// taskSource reads the record a kept copy is made from, in the caller's transaction
// and under the caller's own grants: a task this person may not read is not a task
// they may keep, and the read is done by the desk that owns the row rather than by a
// second reader over its table.
//
// The key is the task's own id in the text the record publishes, and the version its
// own write counter: both are what the record screen showed, which is the only reason
// a control could post them.
type taskSource struct{ tasks taskcontracts.Reader }

func (s taskSource) Capture(ctx context.Context, tx db.Tx[db.Tenant], key, expectedVersion string) (savedcontracts.Capture, error) {
	task, err := s.read(ctx, tx, key)
	if err != nil {
		return savedcontracts.Capture{}, err
	}
	version := strconv.FormatInt(task.Revision, 10)
	if version != expectedVersion {
		return savedcontracts.Capture{}, savedcontracts.ErrStale
	}
	values, err := json.Marshal(task)
	if err != nil {
		return savedcontracts.Capture{}, err
	}
	return savedcontracts.Capture{
		Title: task.Title, Locale: requestLanguage(ctx), Version: version, Values: values,
	}, nil
}

func (s taskSource) Check(ctx context.Context, tx db.Tx[db.Tenant], key string) error {
	_, err := s.read(ctx, tx, key)
	return err
}

// Open is the desk's own screen for this task — the address the workspace mounts,
// not one built out of the retained snapshot's values.
func (s taskSource) Open(ctx context.Context, tx db.Tx[db.Tenant], key string) (string, error) {
	if _, err := s.read(ctx, tx, key); err != nil {
		return "", err
	}
	return "/app/task/tasks/" + key, nil
}

func (s taskSource) read(ctx context.Context, tx db.Tx[db.Tenant], key string) (*taskcontracts.Task, error) {
	id, err := uuid.Parse(key)
	if err != nil {
		return nil, savedcontracts.ErrInvalid
	}
	task, err := s.tasks.TaskByID(ctx, tx, id)
	switch {
	case errors.Is(err, nil):
		return task, nil
	case errors.Is(err, crud.ErrNotFound):
		return nil, savedcontracts.ErrUnavailable
	default:
		return nil, err
	}
}

// SavingState asks the service for this person's copy of this row and the two grants
// as they stand right now. A copy nobody kept is not an error and the caller says so
// by drawing an unpressed control; a grant read that failed is an error, and the
// caller leaves the heart off rather than offering a press that will be refused.
func (p *keepingDoor) SavingState(ctx context.Context, module, kind, key string) (*resource.SavingControl, error) {
	tx, err := savingTransaction(ctx)
	if err != nil {
		return nil, err
	}
	keep, err := p.holds(ctx, tx, savedcontracts.PermissionWrite)
	if err != nil {
		return nil, err
	}
	list, err := p.holds(ctx, tx, savedcontracts.PermissionRead)
	if err != nil {
		return nil, err
	}
	control := &resource.SavingControl{CanSave: keep, CanRemove: keep, SaveURL: p.at.Save}
	if list {
		control.ListURL = p.at.Filtered(module, kind)
	}
	view, err := p.service.Find(ctx, tx, savedcontracts.Ref{Module: module, Kind: kind, Key: key})
	switch {
	case errors.Is(err, savedcontracts.ErrNotFound):
		return control, nil
	case err != nil:
		return nil, err
	}
	control.Pressed = true
	control.ItemID = view.ID.String()
	control.Revision = view.Revision
	control.UnsaveURL = p.at.RemoveAt(control.ItemID)
	return control, nil
}

// holds asks whether this caller has one grant, and keeps the two answers apart: a
// refusal is the answer "no control", and an error is the answer "nothing is known",
// which is the difference between a record page drawn without a heart and one handed
// to the fault chrome.
func (p *keepingDoor) holds(ctx context.Context, tx db.Tx[db.Tenant], permission string) (bool, error) {
	_, err := p.access.Current(ctx, tx, permission)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, savedcontracts.ErrDenied):
		return false, nil
	default:
		return false, err
	}
}

func (p *keepingDoor) Kept(ctx context.Context, module, kind string, limit, offset int) (uisaved.Page, error) {
	tx, err := savingTransaction(ctx)
	if err != nil {
		return uisaved.Page{}, err
	}
	page, err := p.service.List(ctx, tx, savedcontracts.Query{Module: module, Kind: kind, Limit: limit, Offset: offset})
	if err != nil {
		return uisaved.Page{}, savingFault(err)
	}
	out := uisaved.Page{Total: page.Total}
	for _, view := range page.Items {
		row := uisaved.Row{
			ID: view.ID.String(), Module: view.Source.Module, Kind: view.Source.Kind, Key: view.Source.Key,
			Title: view.Snapshot.Title, Captured: view.Snapshot.CapturedAt,
			Availability: view.Availability, Revision: view.Revision,
		}
		// The live record is reached by asking the source again, which reauthorizes;
		// an address kept with the copy would be a link to whatever the record was
		// when it was captured, and to nothing at all once it is gone.
		if view.Availability == uisaved.AvailabilityAvailable {
			if at, err := p.service.Open(ctx, tx, row.ID); err == nil {
				row.SourceURL = at
			}
		}
		out.Rows = append(out.Rows, row)
	}
	return out, nil
}

func (p *keepingDoor) Keep(ctx context.Context, module, kind, key, expectedVersion string) error {
	tx, err := savingTransaction(ctx)
	if err != nil {
		return err
	}
	if _, err := p.service.Save(ctx, tx, savedcontracts.Save{
		Source: savedcontracts.Ref{Module: module, Kind: kind, Key: key}, ExpectedSourceVersion: expectedVersion,
	}); err != nil {
		return savingFault(err)
	}
	return nil
}

func (p *keepingDoor) GiveBack(ctx context.Context, id string, expectedRevision int64) error {
	tx, err := savingTransaction(ctx)
	if err != nil {
		return err
	}
	parsed, err := uuid.Parse(id)
	if err != nil {
		return problem.New(http.StatusUnprocessableEntity, "this is no saved item's id")
	}
	if err := p.service.Unsave(ctx, tx, savedcontracts.Unsave{ID: parsed.String(), ExpectedRevision: expectedRevision}); err != nil {
		return savingFault(err)
	}
	return nil
}

// savingTransaction is the caller's transaction, or the refusal the page renders in
// its own chrome: a copy is kept in the caller's transaction or not at all, and a
// route that reached no transaction was mounted outside the request stack.
func savingTransaction(ctx context.Context) (db.Tx[db.Tenant], error) {
	tx, ok := httpx.TxFrom(ctx)
	if !ok {
		return db.Tx[db.Tenant]{}, problem.New(http.StatusServiceUnavailable, "this request carries no transaction to keep a copy in")
	}
	return tx, nil
}

// savingFault is the service's six refusals in the language a browser page answers
// with. The service's own classification is the one that decides; this only says how
// loudly the page says it, and keeps a copy nobody may see out of the answer.
func savingFault(err error) error {
	switch {
	case errors.Is(err, savedcontracts.ErrDenied):
		return problem.New(http.StatusForbidden, "you may not keep this copy")
	case errors.Is(err, savedcontracts.ErrNotFound):
		// A copy of somebody else's is not this caller's to know about, and the
		// service already refused to distinguish it from absent.
		return problem.NotFound("nothing you saved answers to that")
	case errors.Is(err, savedcontracts.ErrStale):
		return problem.Conflict("the record has moved since this page showed it; read it again")
	case errors.Is(err, savedcontracts.ErrInvalid):
		return problem.New(http.StatusUnprocessableEntity, "this save names no record this application can keep")
	case errors.Is(err, savedcontracts.ErrUnavailable):
		return problem.New(http.StatusConflict, "that record is gone, and the copy of it is yours to remove")
	case errors.Is(err, savedcontracts.ErrTemporary):
		return problem.New(http.StatusServiceUnavailable, "the record could not be read just now; try again")
	default:
		return err
	}
}

// requestLanguage is the tenant locale a retained copy is stamped with, which is the
// language the captured title was written in and not the reader's. The tenant's own
// default is the only thing a write made inside a transaction can honestly name.
func requestLanguage(ctx context.Context) string {
	tenant, ok := tenancy.FromContext(ctx)
	if !ok || tenant.Languages == nil {
		return "en"
	}
	return tenant.Languages.Default
}
