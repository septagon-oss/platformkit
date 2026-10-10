package rest_test

// hints_catalogue_test.go is the whole propagation path, end to end: a hint
// declared on a Spec, a command or a Singleton has to reach the document a shell
// parses. Every hop between the declaration and the body is one assignment
// somebody wrote down, which is exactly the defect class these two cases catch —
// a carrier field nobody copied fails nothing but the screen a person reads.

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// TenantSetting is the singleton: one row per tenant, and the only path a module of
// the fleet reaches with entry-level hints.
type TenantSetting struct {
	crud.Base
	Theme string `json:"theme" ui:"label:Theme"`
}

func (*TenantSetting) TableName() string { return "rest_hint_settings" }

// servedCatalog mounts s, asks the API for its own resources inside a request,
// and answers the entry the way ui/screens would serve it. The API comes back
// with the document, because the half that adds a command to a mounted resource
// needs the same surfaces rather than a second composition.
func servedCatalog(t *testing.T, s rest.Spec[*Task]) (func() screens.Catalog, *httpx.API) {
	t.Helper()
	api, router, _ := mountAs(t, s, caller{})
	var out screens.Catalog
	httpx.Register(api.Surfaces(s.Module).App, huma.Operation{
		OperationID: "catalog-probe", Method: http.MethodPost, Path: "/catalog-probe",
		Hidden: true, DefaultStatus: http.StatusNoContent,
	}, httpx.SignedIn(), func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		out = screens.Describe(ctx, api.Resources())
		return nil, nil
	})
	path := api.Surfaces(s.Module).App.Path("/catalog-probe")
	ask := func() screens.Catalog {
		if code, body := call(t, router, http.MethodPost, path, ""); code != http.StatusNoContent {
			t.Fatalf("the probe = %d %s", code, body)
		}
		return out
	}
	return ask, api
}

// entry is the one served entry a module owns.
func entry(t *testing.T, served screens.Catalog, module string) screens.Entry {
	t.Helper()
	for _, e := range served.Resources {
		if e.Module == module {
			return e
		}
	}
	t.Fatalf("the catalogue holds %v, and no entry for %q", served.Resources, module)
	return screens.Entry{}
}

// TestTheServedCatalogCarriesEveryHint is the case a carrier field nobody copied
// fails. Entry, command and field hints are each declared, and each is read back
// out of the served body rather than out of the value that declared it.
func TestTheServedCatalogCarriesEveryHint(t *testing.T) {
	hinted := spec
	hinted.Present = entity.EntryHints{
		Singular: "task", Plural: "tasks", Description: "What the firm owes somebody",
		Icon: "task", Group: &entity.ResourceGroup{Key: "practice", Label: "Practice"},
		Order: 7, PrimaryField: "title", StatusField: "status",
		SummaryFields:    []string{"dueAt"},
		Sections:         []entity.EntitySection{{Key: "timing", Label: "Timing"}},
		EmptyDescription: "Nothing is owed yet.",
		Sortable:         []string{"title", "dueAt"},
	}
	ask, api := servedCatalog(t, hinted)
	served := ask()
	e := entry(t, served, "tasks")
	p := e.Presentation
	if p == nil {
		t.Fatal("the served entry carries no presentation block")
	}
	if *p.Singular != "task" || *p.Plural != "tasks" || *p.Icon != "task" || *p.Order != 7 ||
		p.Group.Key != "practice" || *p.PrimaryField != "title" || *p.StatusField != "status" ||
		(*p.Sortable)[0] != "title" || (*p.Sections)[0].Key != "timing" ||
		*p.EmptyDescription != "Nothing is owed yet." || (*p.SummaryFields)[0] != "dueAt" {
		t.Errorf("the served entry carries %v, which is not what the Spec declared", p)
	}

	// The command's hints travel the same path, through rest.Command's own
	// options rather than the Spec's.
	rest.Command(api.Surfaces(spec.Module), hinted, "recheck-sla", "Recheck the SLA", "Ask whether it slipped.", nil,
		func(context.Context, db.Tx[db.Tenant], uuid.UUID, struct{}) (*Task, error) { return nil, nil },
		rest.CommandOptions{Present: entity.CommandHints{Label: "Recheck", Destructive: true,
			Confirmation: &entity.CommandConfirmation{Title: "Recheck the SLA?",
				Body: "Nothing changes until somebody answers.", ConfirmLabel: "Recheck"},
			SuccessMessage: "The recheck is asked for."}})
	served = ask()
	var command *screens.Command
	e = entry(t, served, "tasks")
	for i, c := range e.Commands {
		if c.Verb == "recheck-sla" {
			command = &e.Commands[i]
		}
	}
	if command == nil {
		t.Fatal("the served entry carries no recheck-sla command")
	}
	if command.Presentation == nil || *command.Presentation.Label != "Recheck" ||
		command.Presentation.Confirmation.ConfirmLabel != "Recheck" ||
		command.Presentation.Confirmation.Body == "" || command.Presentation.Destructive == nil || *command.Presentation.Destructive != true ||
		*command.Presentation.SuccessMessage != "The recheck is asked for." {
		t.Errorf("the served command carries %v, not what its options declared", command.Presentation)
	}
}

// TestASingletonCarriesItsPresentation is the path modules/site reaches: a
// Singleton registers a resource and nothing else, so a gate or a wire wired only
// into Spec would leave a tenant's one row of settings described by the kernel's
// own guesses.
func TestASingletonCarriesItsPresentation(t *testing.T) {
	singleton := rest.Singleton[*TenantSetting]{
		Module: "site", Entity: "setting", Path: "/settings",
		Read: "site:read", Write: "site:write",
		Load: func(context.Context, db.Tx[db.Tenant]) (*TenantSetting, error) { return &TenantSetting{}, nil },
		Save: func(_ context.Context, _ db.Tx[db.Tenant], in *TenantSetting) (*TenantSetting, error) { return in, nil },
		Present: entity.EntryHints{
			Singular: "site settings", Description: "What this tenant's pages say about themselves",
			Icon: "settings", PrimaryField: "theme",
			Sections: []entity.EntitySection{{Key: "identity", Label: "Identity"}},
		},
	}
	ask, api := servedCatalog(t, spec)
	singleton.Mount(api.Surfaces("site"))
	served := ask()
	e := entry(t, served, "site")
	if !e.Singleton {
		t.Error("the entry no longer says a tenant has one of these")
	}
	if e.Presentation == nil || *e.Presentation.Singular != "site settings" || *e.Presentation.Icon != "settings" ||
		*e.Presentation.PrimaryField != "theme" || (*e.Presentation.Sections)[0].Key != "identity" {
		t.Errorf("the singleton's entry carries %v", e.Presentation)
	}
	// The field level arrives on the same schema the singleton derived for itself.
	raw, err := json.Marshal(e.Fields)
	if err != nil {
		t.Fatal(err)
	}
	var fields []struct {
		Name         string             `json:"name"`
		Presentation *entity.FieldHints `json:"presentation"`
	}
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, f := range fields {
		if f.Name == "theme" && (f.Presentation == nil || f.Presentation.Label != "Theme") {
			t.Errorf("the singleton's field carries %v, want the declared label", f.Presentation)
		}
	}
}

// TestASingletonRefusesAnInvalidHintNamingIt: the gate is the same gate.
func TestASingletonRefusesAnInvalidHintNamingIt(t *testing.T) {
	hintRefused(t, func() {
		rest.Singleton[*TenantSetting]{Module: "site", Entity: "setting", Path: "/settings",
			Read: "site:read", Write: "site:write",
			Load:    func(context.Context, db.Tx[db.Tenant]) (*TenantSetting, error) { return &TenantSetting{}, nil },
			Save:    func(_ context.Context, _ db.Tx[db.Tenant], in *TenantSetting) (*TenantSetting, error) { return in, nil },
			Present: entity.EntryHints{Icon: "gearwheel"},
		}.Mount(httpx.Surfaces{})
	}, `icon "gearwheel"`, "entity.Icons")
}
