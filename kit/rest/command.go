package rest

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"net/http"
	"reflect"
	"slices"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
)

// CommandOptions is what a command may differ from its Spec in. It is a struct
// and not two more parameters because a command that differs in nothing has to
// be able to say so in one word: rest.CommandOptions{}.
//
// What a command writes is deliberately not among the things it can say here.
// The kernel cannot know it without running the command, and running it is the
// write; a list copied onto the command would be a second, editable copy of a
// fact the module already holds, and one the author could simply forget to
// write. The door asks the module by verb instead — Spec.Gate says how.
type CommandOptions struct {
	Choices *entity.ChoiceFields
	// MintsCredential prevents retained responses in JSON and generated forms.
	// Undoable commands also declare this automatically for their rewind token.
	MintsCredential bool
	// Bindings copies observed row fields into named command arguments. It is
	// presentation metadata; execution validates submitted values without reload.
	Bindings *RowBindings
	// Undo names the authoritative token command for result-bearing commands.
	// Empty means no undo capability is advertised.
	Undo string
	// Auth is who may run this command. The zero value is the Spec's own write
	// permission, which is right for a command that moves a row an
	// administrator owns and wrong for one somebody runs on their own thing:
	// adding an item to a basket is httpx.SignedIn(), and the alternative was a
	// second permission every tenant would have to grant to every shopper.
	Auth httpx.Auth

	// MapArgs declares named map arguments and their permitted keys for generated
	// forms. Invalid declarations panic before mounting. The pointer preserves
	// CommandOptions comparability; nil declares no maps.
	MapArgs *MapArgs

	// Collection mounts POST {Path}/{verb} instead of a row route and passes
	// uuid.Nil to run, for commands that find or create their own row.
	Collection bool

	// Self says the caller is the subject of the command: no argument names the
	// row it writes, because the row is whoever is signed in.
	//
	// It changes no guard — the route's own Auth answers who may call — and it
	// changes one thing on the screen. A generated list offers the commands its
	// rows may have run on them, and a command about the person reading the list
	// is not about any row on it: rendered beside "New User" it reads as a write
	// on the people listed, and a person who pressed it would have changed their
	// own account. So the generated screens leave it off, and the account is the
	// surface that offers it (modules/admin's account menu) or the API is.
	Self bool

	// Present is this command as its author describes it: the word on the button,
	// whether it is the one action a view should offer, whether it costs
	// something, what a person reads before it runs and what they are told after.
	// A command no person is offered says `System: true`, and ui/screens mounts no
	// browser route for it — the JSON route keeps its guard unchanged.
	Present entity.CommandHints
}

// Command registers a lifecycle POST on a row, or the collection when requested,
// using the Spec's write guard unless overridden and the request transaction.
// An absent body uses declared defaults or zero I; run validates domain inputs.
// Failures use Fault. Server-owned argument fields panic before registration;
// use caller-owned names for revision conditions. See README.md for composition.
func Command[I any, T crud.Entity](surfaces httpx.Surfaces, spec Spec[T], verb, summary, description string, events []string,
	run func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in I) (T, error), opts CommandOptions,
) {
	registerCommand(surfaces, spec, verb, summary, description, events, run, opts, nil, false, func(out T) httpx.CommandReply { return httpx.CommandReply{Result: out} })
}

// CommandResult mounts a command whose authoritative output is independent of
// its resource row. JSON and generated adapters retain Result and UndoToken;
// errors return no reply. Undo is available only when opts names its command.
func CommandResult[I any, T crud.Entity, O any](surfaces httpx.Surfaces, spec Spec[T], verb, summary, description string, events []string,
	run func(context.Context, db.Tx[db.Tenant], uuid.UUID, I) (Response[O], error), opts CommandOptions,
) {
	registerCommand(surfaces, spec, verb, summary, description, events, run, opts, entity.FieldsOf(reflect.TypeFor[O]()), true, func(out Response[O]) httpx.CommandReply {
		return httpx.CommandReply{Result: out.Result, UndoToken: out.UndoToken}
	})
}

func registerCommand[I any, T crud.Entity, O any](surfaces httpx.Surfaces, spec Spec[T], verb, summary, description string, events []string,
	run func(context.Context, db.Tx[db.Tenant], uuid.UUID, I) (O, error), opts CommandOptions, output []entity.Field, result bool, reply func(O) httpx.CommandReply,
) {
	// A command's hints are checked here rather than in Spec.check because this is
	// the one site with the verb, the options, the entity's schema and the
	// argument's fields all in hand at once — and before any route is mounted, so
	// a refused declaration leaves no half-described resource behind it.
	if bad := commandFault(verb, opts.Present, crud.FieldsOf(reflect.TypeFor[I]())); bad != "" {
		panic("rest: command " + verb + " on " + spec.Module + "." + spec.Entity + ": " + bad)
	}
	router, op, auth, credential := commandRoute(surfaces, spec, verb, summary, description, events, opts)
	fields, prepare, emptyFault, bindings := commandInputs[I](spec, verb, opts)
	if opts.Undo != "" && !result {
		panic("rest: undo requires a result-bearing command")
	}
	execute := commandExecutor(spec, verb, opts, prepare, run, reply)
	// Endpoint is the command's own absolute address, which is the one path a
	// shell can no longer derive for itself: once a command may live on the
	// control-plane surface, {path}/{id}/{verb} stops being the rule. The
	// catalog publishes it for exactly that reason — see screens.Command.Path.
	surfaces.AddCommand(spec.Module, spec.Entity, httpx.Command{
		Verb: verb, Summary: summary, Description: description,
		Collection: opts.Collection, Self: opts.Self, Auth: auth, MintsCredential: credential,
		Endpoint: router.Prefix() + op.Path,
		Fields:   fields, EmptyInputFault: emptyFault, Present: opts.Present,
		Bindings: bindings, Undo: opts.Undo, HasResult: result, Output: output, Execute: execute,
		Run: func(ctx context.Context, id uuid.UUID, values map[string]any) error {
			_, err := execute(ctx, id, values)
			return err
		},
	})
	mountCommand(router, op, auth, spec, verb, opts, fields, bindings, prepare, run, result)
}

// MapArgs is a command's map-valued arguments, keyed by the JSON name the body uses
// for them, and declared by whoever owns the command.
type MapArgs struct {
	// Args is one declaration per argument: the shape Go cannot state (what each
	// key's value is) and the keys no reflection can discover.
	Args map[string]crud.MapArg
}

// declareMapArgs puts each declared map argument in beside the fields the schema
// derived, in the order the argument struct declares them, and refuses a
// declaration describing an argument that is not a map keyed by a string: a key
// list beside an argument that would ignore it is the bug this option exists to
// cure, and a mount-time refusal is the only answer that cannot ship.
func declareMapArgs(in reflect.Type, fields []crud.Field, declared *MapArgs) ([]crud.Field, error) {
	var args map[string]crud.MapArg
	if declared != nil {
		args = declared.Args
	}
	if len(args) == 0 {
		return fields, nil
	}
	if in.Kind() == reflect.Pointer {
		in = in.Elem()
	}
	seen := map[string]bool{}
	for _, sf := range reflect.VisibleFields(in) {
		declared, ok := args[jsonNameOf(sf)]
		if !ok {
			continue
		}
		seen[jsonNameOf(sf)] = true
		t := sf.Type
		if t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if t.Kind() != reflect.Map || t.Key().Kind() != reflect.String {
			return nil, fmt.Errorf("command argument %q is declared as a map of %s and is %s",
				jsonNameOf(sf), declared.Elem, sf.Type)
		}
		fields = append(fields, crud.Field{
			Name: jsonNameOf(sf), Type: crud.TypeMap, Elem: declared.Elem,
			Doc: cmp.Or(declared.Doc, sf.Tag.Get("doc")), Keys: slices.Clone(declared.Keys),
			Default: sf.Tag.Get("default"),
			Index:   sf.Index,
		})
	}
	if len(args) > 0 {
		leftover := make([]string, 0, len(args))
		for name := range args {
			if !seen[name] {
				leftover = append(leftover, name)
			}
		}
		if len(leftover) == 0 {
			return fields, nil
		}
		slices.Sort(leftover)
		return nil, fmt.Errorf("command declares map arguments %s and takes none of them",
			strings.Join(leftover, ", "))
	}
	return fields, nil
}

// jsonNameOf is the name a struct field is addressed by, which is its json tag and
// otherwise its Go name — the same rule entity/jsonName reads, asked of a field
// that is deliberately absent from the derived schema.
func jsonNameOf(sf reflect.StructField) string {
	name, _, _ := strings.Cut(sf.Tag.Get("json"), ",")
	if name == "" {
		return sf.Name
	}
	return name
}

// commandInput is a command's path id and its body. The body is a pointer
// because huma reads a struct body as required, and "{}" is not something a
// caller should have to send to say nothing.
type commandInput[I any] struct {
	ID   uuid.UUID `path:"id" format:"uuid" doc:"The row's id"`
	Body *I
}

// collectionInput is the same without the id, for a command whose path has no
// {id} to bind.
type collectionInput[I any] struct {
	Body *I
}

func commandRoute[T crud.Entity](
	surfaces httpx.Surfaces, spec Spec[T], verb, summary, description string,
	events []string, opts CommandOptions,
) (*httpx.Router, huma.Operation, httpx.Auth, bool) {
	path := spec.item() + "/" + verb
	if opts.Collection {
		path = strings.TrimSuffix(spec.Path, "/") + "/" + verb
	}
	auth := spec.writeAuth()
	if opts.Auth.Declared() {
		auth = opts.Auth
	}
	// A command the installation owns is a control-plane route, whatever the
	// resource it acts on is otherwise: the guard is the fact, and the surface
	// follows the guard rather than the other way round.
	router := surfaces.App
	if auth.Operator() {
		router = surfaces.Ops
	}
	op := huma.Operation{
		OperationID: spec.Module + "-" + spec.Entity + "-" + verb,
		Method:      http.MethodPost,
		Path:        path,
		Summary:     summary,
		Description: description,
		Tags:        []string{spec.Module},
		Errors:      faults,
	}
	if len(events) > 0 {
		op.Extensions = map[string]any{httpx.EventsExtension: events}
	}
	credential := opts.MintsCredential || opts.Undo != ""
	if credential {
		if op.Extensions == nil {
			op.Extensions = map[string]any{}
		}
		op.Extensions[httpx.CredentialExtension] = true
	}
	return router, op, auth, credential
}

func commandInputs[I any, T crud.Entity](
	spec Spec[T], verb string, opts CommandOptions,
) ([]crud.Field, func(map[string]any) (I, error), string, map[string]string) {
	// The same declaration, recorded on the resource, so a shell generated
	// from the catalog offers this door rather than inventing one — and recorded
	// with the work beside it, because "offers this door" was always a promise
	// this line could not keep. A screen's write goes through the closure below,
	// so the form and the JSON route perform one implementation through one
	// guard. See docs/adr/0007 and httpx.Command.Run.
	fields := crud.FieldsOf(reflect.TypeFor[I]())
	fields, err := declareMapArgs(reflect.TypeFor[I](), fields, opts.MapArgs)
	if err != nil {
		panic("rest: " + err.Error())
	}
	fields, err = entity.WithChoices(fields, opts.Choices)
	if err == nil {
		err = choiceTypes(reflect.TypeFor[I](), fields)
	}
	if err != nil {
		panic("rest: command " + verb + ": " + err.Error())
	}
	prepare, emptyFault, err := commandDefaults[I](fields)
	if err != nil {
		panic("rest: command " + verb + " on " + spec.Module + "." + spec.Entity + ": " + err.Error())
	}
	bindings := map[string]string(nil)
	if opts.Bindings != nil {
		bindings = maps.Clone(opts.Bindings.Fields)
	}
	if bad := httpx.BindingFault(fields, spec.Schema().Fields, bindings); bad != "" {
		panic("rest: command " + verb + ": " + bad)
	}
	if len(bindings) > 0 {
		decode := prepare
		prepare = func(values map[string]any) (I, error) {
			for _, name := range slices.Sorted(maps.Keys(bindings)) {
				if value, ok := values[name]; !ok || value == nil {
					var zero I
					return zero, fmt.Errorf("%w: bound argument %q is required", crud.ErrInvalid, name)
				}
			}
			return decode(values)
		}
	}
	return fields, prepare, emptyFault, bindings
}

func commandExecutor[I any, T crud.Entity, O any](
	spec Spec[T], verb string, opts CommandOptions, prepare func(map[string]any) (I, error),
	run func(context.Context, db.Tx[db.Tenant], uuid.UUID, I) (O, error), reply func(O) httpx.CommandReply,
) func(context.Context, uuid.UUID, map[string]any) (httpx.CommandReply, error) {
	return func(ctx context.Context, id uuid.UUID, values map[string]any) (httpx.CommandReply, error) {
		tx, ok := httpx.TxFrom(ctx)
		if !ok {
			return httpx.CommandReply{}, problem.New(http.StatusServiceUnavailable, "this command is only performed inside a request")
		}
		in, err := prepare(values)
		if err != nil {
			return httpx.CommandReply{}, err
		}
		if err := spec.commandGate(ctx, tx, id, verb, opts); err != nil {
			return httpx.CommandReply{}, err
		}
		out, err := run(ctx, tx, id, in)
		if err != nil {
			return httpx.CommandReply{}, err
		}
		return reply(out), nil
	}
}

func mountCommand[I any, T crud.Entity, O any](
	router *httpx.Router, op huma.Operation, auth httpx.Auth, spec Spec[T], verb string,
	opts CommandOptions, fields []crud.Field, bindings map[string]string,
	prepare func(map[string]any) (I, error), run func(context.Context, db.Tx[db.Tenant], uuid.UUID, I) (O, error),
	result bool,
) {
	// The two mounts differ in their input type and in nothing else, and the
	// type is what tells huma whether there is a path parameter to bind. That
	// is the irreducible half of the difference; everything above it and the
	// answer below are shared.
	answer := func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, body *I) (O, error) {
		var in I
		if body != nil {
			in = *body
		}
		if err := spec.commandGate(ctx, tx, id, verb, opts); err != nil {
			var zero O
			return zero, err
		}
		return run(ctx, tx, id, in)
	}
	responseOptions := OperationOptions{}
	if result {
		responseOptions.CacheControl = "private, no-store"
	}
	if hasChoices(fields) || len(bindings) > 0 || slices.ContainsFunc(fields, func(f crud.Field) bool { return f.Default != "" }) {
		mountDefaultCommand(router, op, auth, opts.Collection, prepare, answer, responseOptions)
		return
	}
	if opts.Collection {
		Operation(router, op, auth, func(ctx context.Context, tx db.Tx[db.Tenant], _ uuid.UUID, in *collectionInput[I]) (O, error) {
			return answer(ctx, tx, uuid.Nil, in.Body)
		}, responseOptions)
		return
	}
	Operation(router, op, auth, func(ctx context.Context, tx db.Tx[db.Tenant], _ uuid.UUID, in *commandInput[I]) (O, error) {
		return answer(ctx, tx, in.ID, in.Body)
	}, responseOptions)
}
