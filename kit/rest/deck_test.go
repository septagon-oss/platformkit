package rest_test

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strings"
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

func deckSpec() rest.Spec[*Task] {
	s := spec
	s.Present = entity.EntryHints{Sortable: []string{"title"}, Deck: &entity.DeckHints{
		Queue: []string{"status:open"}, Order: "title", Directions: map[string]entity.DeckDirection{
			"right": {Command: "publish", Label: "Publish", Tone: "success", Icon: "document"},
			"left":  {Command: "archive", Label: "Archive", Tone: "warning", Icon: "folder"},
			"down":  {Skip: true},
		},
	}}
	return s
}

func TestInvalidDeckRefusesSpecAndSingletonMount(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*rest.Spec[*Task])
		want   string
	}{
		{"queue", func(s *rest.Spec[*Task]) { s.Present.Deck.Queue = []string{"priority:bad"} }, "deck.queue"},
		{"order", func(s *rest.Spec[*Task]) { s.Present.Deck.Order = "missing" }, "deck.order"},
		{"direction", func(s *rest.Spec[*Task]) { s.Present.Deck.Directions["up"] = entity.DeckDirection{Skip: true} }, "deck.directions.up"},
		{"no list", func(s *rest.Spec[*Task]) { s.Operations = []httpx.CRUD{rest.Read} }, "deck requires a list"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := deckSpec()
			tc.change(&s)
			defer func() {
				if got := fmt.Sprint(recover()); !strings.Contains(got, tc.want) || !strings.Contains(got, "tasks.task") {
					t.Fatalf("mount refusal = %s", got)
				}
			}()
			s.Mount(httpx.Surfaces{})
			t.Fatal("invalid deck mounted")
		})
	}
	t.Run("singleton", func(t *testing.T) {
		defer func() {
			if got := fmt.Sprint(recover()); !strings.Contains(got, "deck requires a list") {
				t.Fatal(got)
			}
		}()
		rest.Singleton[*Task]{Name: rest.Name{One: "Task", Other: "Tasks"}, Module: "tasks", Entity: "task", Path: "/task", Read: "task:read", Present: deckSpec().Present, Load: func(context.Context, db.Tx[db.Tenant]) (*Task, error) { return nil, nil }}.Mount(httpx.Surfaces{})
		t.Fatal("singleton deck mounted")
	})
}

func TestDeckCommandGateUsesCompleteRegistrationInEitherOrder(t *testing.T) {
	for _, before := range []bool{false, true} {
		t.Run(fmt.Sprint(before), func(t *testing.T) {
			s := deckSpec()
			api, _ := patchDocumentAPI()
			add := func() {
				run := func(context.Context, db.Tx[db.Tenant], uuid.UUID, struct{}) (*Task, error) { return nil, nil }
				rest.Command(api.Surfaces(s.Module), s, "publish", "Publish", "", nil, run, rest.CommandOptions{})
				rest.Command(api.Surfaces(s.Module), s, "archive", "Archive", "", nil, run, rest.CommandOptions{})
			}
			if before {
				add()
			}
			s.Mount(api.Surfaces(s.Module))
			if !before {
				add()
			}
			if err := api.ValidateDeclarations(); err != nil {
				t.Fatal(err)
			}
			resources := api.Resources()
			resources[0].Present.Deck = resources[0].Present.Deck.Clone()
			v := resources[0].Present.Deck.Directions["right"]
			v.Command = "vanish"
			resources[0].Present.Deck.Directions["right"] = v
			if bad := rest.CheckRowHints(resources); !strings.Contains(bad, "tasks/task deck.directions.right.command") || !strings.Contains(bad, "vanish") {
				t.Fatal(bad)
			}
		})
	}
}

func TestDeckRefusesCommandsThatNeedInputOrCannotActOnARow(t *testing.T) {
	type optional struct {
		Note string `json:"note,omitempty"`
	}
	type unsupported struct {
		Details struct{ Note string } `json:"details"`
	}
	type mapInput struct {
		Values map[string]string `json:"values"`
	}
	for _, tc := range []struct {
		name     string
		register func(httpx.Surfaces, rest.Spec[*Task])
		want     string
	}{
		{"optional is not defaulted", func(m httpx.Surfaces, s rest.Spec[*Task]) {
			rest.Command(m, s, "publish", "Publish", "", nil, func(context.Context, db.Tx[db.Tenant], uuid.UUID, optional) (*Task, error) { return nil, nil }, rest.CommandOptions{})
		}, "note"},
		{"unsupported input", func(m httpx.Surfaces, s rest.Spec[*Task]) {
			rest.Command(m, s, "publish", "Publish", "", nil, func(context.Context, db.Tx[db.Tenant], uuid.UUID, unsupported) (*Task, error) { return nil, nil }, rest.CommandOptions{})
		}, "details"},
		{"undeclared map", func(m httpx.Surfaces, s rest.Spec[*Task]) {
			rest.Command(m, s, "publish", "Publish", "", nil, func(context.Context, db.Tx[db.Tenant], uuid.UUID, mapInput) (*Task, error) { return nil, nil }, rest.CommandOptions{})
		}, "values"},
		{"declared map needs default", func(m httpx.Surfaces, s rest.Spec[*Task]) {
			rest.Command(m, s, "publish", "Publish", "", nil, func(context.Context, db.Tx[db.Tenant], uuid.UUID, mapInput) (*Task, error) { return nil, nil }, rest.CommandOptions{MapArgs: &rest.MapArgs{Args: map[string]crud.MapArg{"values": {Elem: crud.TypeString, Keys: []entity.MapKey{{Name: "title"}}}}}})
		}, "values"},
		{"system", func(m httpx.Surfaces, s rest.Spec[*Task]) {
			rest.Command(m, s, "publish", "Publish", "", nil, func(context.Context, db.Tx[db.Tenant], uuid.UUID, struct{}) (*Task, error) { return nil, nil }, rest.CommandOptions{Present: entity.CommandHints{System: true}})
		}, "system"},
		{"collection", func(m httpx.Surfaces, s rest.Spec[*Task]) {
			rest.Command(m, s, "publish", "Publish", "", nil, func(context.Context, db.Tx[db.Tenant], uuid.UUID, struct{}) (*Task, error) { return nil, nil }, rest.CommandOptions{Collection: true})
		}, "collection"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := deckSpec()
			delete(s.Present.Deck.Directions, "left")
			api, _ := patchDocumentAPI()
			s.Mount(api.Surfaces(s.Module))
			tc.register(api.Surfaces(s.Module), s)
			if err := api.ValidateDeclarations(); err == nil || !strings.Contains(err.Error(), "deck.directions.right.command") || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("refusal = %v", err)
			}
		})
	}
}

type defaultedCommand struct {
	Note    string           `json:"note" default:"done"`
	Count   int64            `json:"count" default:"9007199254740993"`
	Enabled bool             `json:"enabled" default:"true"`
	Tags    []string         `json:"tags" default:"[\"pending\"]"`
	Values  map[string]int64 `json:"values" default:"{\"rank\":9007199254740993}"`
	Ignored struct{}         `json:"-"`
}

func TestDefaultedDeckCommandsReceiveFreshDefaultsThroughBothAdapters(t *testing.T) {
	s := deckSpec()
	delete(s.Present.Deck.Directions, "left")
	var received []defaultedCommand
	api, router, _ := mountAs(t, s, caller{}, func(api *httpx.API) {
		rest.Command(api.Surfaces(s.Module), s, "publish", "Publish", "", nil,
			func(_ context.Context, _ db.Tx[db.Tenant], _ uuid.UUID, in defaultedCommand) (*Task, error) {
				received = append(received, in)
				if in.Values != nil {
					in.Values["rank"]++
				}
				if len(in.Tags) > 0 {
					in.Tags[0] = "changed"
				}
				return &Task{Title: in.Note}, nil
			}, rest.CommandOptions{MapArgs: &rest.MapArgs{Args: map[string]crud.MapArg{"values": {Elem: crud.TypeInt, Keys: []entity.MapKey{{Name: "rank"}}}}}})
	})
	command := api.Resources()[0].Commands[0]
	f, _ := entity.FieldNamed(command.Fields, "values")
	if f.Default == "" {
		t.Fatal("map default erased")
	}
	httpx.Register(api.Surfaces(s.Module).App, huma.Operation{OperationID: "run-defaults", Method: http.MethodPost, Path: "/run-defaults"}, httpx.SignedIn(), func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		return nil, command.Run(ctx, uuid.New(), map[string]any{})
	})
	for _, body := range []string{"", "{}", "{}"} {
		if code, out := call(t, router, http.MethodPost, "/api/v1/tasks/task/"+uuid.NewString()+"/publish", body); code != http.StatusOK {
			t.Fatalf("%q: %d %s", body, code, out)
		}
	}
	if code, out := call(t, router, http.MethodPost, "/api/v1/tasks/run-defaults", ""); code >= 300 {
		t.Fatalf("Run: %d %s", code, out)
	}
	for i, in := range received {
		if in.Note != "done" || in.Count != 9007199254740993 || !in.Enabled || !reflect.DeepEqual(in.Tags, []string{"changed"}) || in.Values["rank"] != 9007199254740994 {
			t.Fatalf("invocation %d = %+v", i, in)
		}
	}
	if len(received) != 4 {
		t.Fatalf("calls=%d", len(received))
	}
	if code, out := call(t, router, http.MethodPost, "/api/v1/tasks/task/"+uuid.NewString()+"/publish", `{"note":"","count":0,"enabled":false,"tags":null,"values":null}`); code != http.StatusOK {
		t.Fatalf("explicit zero: %d %s", code, out)
	}
	last := received[len(received)-1]
	if last.Note != "" || last.Count != 0 || last.Enabled || last.Tags != nil || last.Values != nil {
		t.Fatalf("supplied values overwritten: %+v", last)
	}
}

func TestServedDeckOmitsOnlyTheForbiddenDirection(t *testing.T) {
	for _, denied := range []string{"task:publish", "task:archive"} {
		t.Run(denied, func(t *testing.T) {
			s := deckSpec()
			api, router, _ := mountAs(t, s, denies{permission: denied}, func(api *httpx.API) {
				run := func(context.Context, db.Tx[db.Tenant], uuid.UUID, struct{}) (*Task, error) { return nil, nil }
				for _, verb := range []string{"publish", "archive"} {
					rest.Command(api.Surfaces(s.Module), s, verb, verb, "", nil, run, rest.CommandOptions{Auth: httpx.Permission("task:" + verb)})
				}
			})
			httpx.Register(api.Surfaces(s.Module).App, huma.Operation{OperationID: "deck-catalog", Method: http.MethodGet, Path: "/deck-catalog"}, httpx.SignedIn(), func(ctx context.Context, _ *struct{}) (*struct{ Body screens.Catalog }, error) {
				return &struct{ Body screens.Catalog }{screens.Describe(ctx, api.Resources())}, nil
			})
			code, body := call(t, router, http.MethodGet, "/api/v1/tasks/deck-catalog", "")
			if code != 200 {
				t.Fatalf("%d %s", code, body)
			}
			if strings.Contains(body, `"command":"`+strings.TrimPrefix(denied, "task:")+`"`) {
				t.Fatal(body)
			}
			if !strings.Contains(body, `"skip":true`) || !strings.Contains(body, `"command":`) {
				t.Fatal(body)
			}
		})
	}
}
