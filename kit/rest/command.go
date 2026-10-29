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
	// Beside a CommandSpec this is the deprecated spelling of CommandSpec.Auth:
	// stated here alone it still mounts, because a guard names itself, and stated
	// two different ways across the two fields the mount refuses.
	Auth httpx.Auth

	// MapArgs declares named map arguments and their permitted keys for generated
	// forms. Invalid declarations panic before mounting. The pointer preserves
	// CommandOptions comparability; nil declares no maps.
	MapArgs *MapArgs

	// Collection mounts POST {Path}/{verb} instead of a row route and passes
	// uuid.Nil to run, for commands that find or create their own row. Beside a
	// CommandSpec this is the deprecated spelling of CommandSpec.Collection, and
	// unlike the guard it cannot be stated here alone: false is how "the row's
	// route" and "nothing said" are both spelled, so a true the CommandSpec does
	// not also say is refused at the mount site rather than guessed at, since a
	// guess would move the command off the path its declaration names.
	Collection bool

	// Present is this command as its author describes it: the word on the button,
	// whether it is the one action a view should offer, whether it costs
	// something, what a person reads before it runs and what they are told after.
	// A command no person is offered says `System: true`, and ui/screens mounts no
	// browser route for it — the JSON route keeps its guard unchanged.
	Present entity.CommandHints
}

// CommandSpec is one lifecycle command as its author declares it: the route it
// is, what the document says about it, what it publishes and who may call it.
//
// What sits here is what decides *which* route exists — the verb, whether the
// path carries an id, and the guard the route is mounted under. How the command
// is drawn and performed stays in Options, along with the deprecated spelling of
// the guard, which still mounts: those two facts pick the route, and Options may
// pick it no other way than the CommandSpec does. A zero Auth is the Spec's write
// declaration — right for a command that moves a row an administrator owns,
// wrong for one somebody runs on their own thing, which is httpx.SignedIn().
type CommandSpec[I any] struct {
	// Verb is the route's last path segment.
	Verb string
	// Summary is the document's one-line summary of the command.
	Summary string
	// Doc is the document's long description of it.
	Doc string
	// Emits names the events this command publishes. kit/app reads the
	// declaration off the mounted operation and refuses a name no manifest holds.
	Emits []string
	// Auth is who may run this command; zero is the Spec's write declaration.
	Auth httpx.Auth
	// Collection mounts POST {Path}/{verb} instead of a row route and passes
	// uuid.Nil to run, for commands that find or create their own row.
	Collection bool
	// Options holds the facts about how the command is drawn and performed. Its
	// Auth is a second word for the guard above and mounts beside it; its
	// Collection is refused beside a CommandSpec, which is the only field that
	// can say which route this command is.
	Options CommandOptions
}

// Mount registers one lifecycle command declared as a CommandSpec: a POST on the
// row the path names, or on the collection when Collection says the command finds
// its own row, under the declared guard or the Spec's write guard, inside the
// request's own transaction. An absent body uses declared defaults or zero I; run
// validates domain inputs. Failures use Fault. A command whose guard or route two
// fields answer differently, or whose arguments the schema cannot describe, panics
// at this site before anything is mounted. Spec's Mount method mounts the five CRUD
// routes beside such a command; this one mounts the command.
//
// Command is this function called with positional parameters, and both forms run
// through one body. Server-owned argument fields panic before registration; use
// caller-owned names for revision conditions. See README.md for composition.
func Mount[I any, T crud.Entity](surfaces httpx.Surfaces, spec Spec[T], cmd CommandSpec[I],
	run func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in I) (T, error),
) {
	registerCommand(surfaces, spec, cmd.Verb, cmd.Summary, cmd.Doc, cmd.Emits, run, declaredCommand(spec, cmd),
		nil, false, func(out T) httpx.CommandReply { return httpx.CommandReply{Result: out} })
}

// MountResult mounts a command whose authoritative output is independent of its
// resource row. JSON and generated adapters retain Result and UndoToken; errors
// return no reply. Undo is available only when Options names its command. See
// Mount for the declaration and the refusal of one stated twice.
func MountResult[I any, T crud.Entity, O any](surfaces httpx.Surfaces, spec Spec[T], cmd CommandSpec[I],
	run func(context.Context, db.Tx[db.Tenant], uuid.UUID, I) (Response[O], error),
) {
	registerCommand(surfaces, spec, cmd.Verb, cmd.Summary, cmd.Doc, cmd.Emits, run, declaredCommand(spec, cmd),
		entity.FieldsOf(reflect.TypeFor[O]()), true, func(out Response[O]) httpx.CommandReply {
			return httpx.CommandReply{Result: out.Result, UndoToken: out.UndoToken}
		})
}

// declaredCommand is a CommandSpec as registerCommand reads it: the two facts that
// pick the route are read out of the declaration, and a value that names either one
// two different ways is refused here, in the same voice as every other wiring rule
// of this package and before a route exists.
func declaredCommand[I any, T crud.Entity](spec Spec[T], cmd CommandSpec[I]) CommandOptions {
	if bad := cmd.routeFault(); bad != "" {
		panic("rest: command " + cmd.Verb + " on " + spec.Module + "." + spec.Entity + ": " + bad)
	}
	opts := cmd.Options
	// The guard may be stated in either field and the refusal above leaves one
	// answer per fact, so it is taken from whichever states it: the top level is
	// where a CommandSpec keeps it, Options is where the positional form found it.
	if !opts.Auth.Declared() {
		opts.Auth = cmd.Auth
	}
	opts.Collection = cmd.Collection
	return opts
}

// routeFault names what this declaration answers two different ways about its own
// route, and "" when every reader of it gets one answer. The guard is comparable,
// so Options may state it alone: a guard names itself, and the positional form
// found it in Options. A bool cannot tell "unset" from false, so the collection
// route cannot: Options.Collection is the deprecated spelling of a fact this
// declaration owns, and a true the CommandSpec does not also say is indistinguishable
// from one contradicted at the top level, where guessing would move the command off
// its own path. Which callers still write the deprecated spelling, and so could reach
// either refusal, is a fact about the tree that moves: docs/compatibility/v3.0.0.md
// counts it there.
func (c CommandSpec[I]) routeFault() string {
	switch {
	case c.Auth.Declared() && c.Options.Auth.Declared() && c.Auth != c.Options.Auth:
		return "states its guard twice: CommandSpec.Auth is " + c.Auth.String() +
			" and Options.Auth is " + c.Options.Auth.String() + "; a command's guard is stated once"
	case c.Options.Collection && !c.Collection:
		return "states its route twice: Options.Collection mounts the collection route where CommandSpec.Collection mounts the row's; a command's route is stated once"
	}
	return ""
}

// Command registers a lifecycle POST on a row, or the collection when requested,
// using the Spec's write guard unless overridden and the request transaction.
// An absent body uses declared defaults or zero I; run validates domain inputs.
// Failures use Fault. Server-owned argument fields panic before registration;
// use caller-owned names for revision conditions. See README.md for composition.
//
// Deprecated: use Mount with a CommandSpec, which names the verb, the summary, the
// description and the events as fields of the declaration rather than as four
// adjacent positional parameters, and keeps the guard and the collection route at
// the top level. Both forms share one body, so the migration is the spelling and
// not the behaviour; see docs/compatibility/v3.0.0.md.
func Command[I any, T crud.Entity](surfaces httpx.Surfaces, spec Spec[T], verb, summary, description string, events []string,
	run func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in I) (T, error), opts CommandOptions,
) {
	Mount(surfaces, spec, declaredInPlace[I](verb, summary, description, events, opts), run)
}

// CommandResult mounts a command whose authoritative output is independent of
// its resource row. JSON and generated adapters retain Result and UndoToken;
// errors return no reply. Undo is available only when opts names its command.
//
// Deprecated: use MountResult with a CommandSpec, which declares the same command
// as one value; see Command and docs/compatibility/v3.0.0.md.
func CommandResult[I any, T crud.Entity, O any](surfaces httpx.Surfaces, spec Spec[T], verb, summary, description string, events []string,
	run func(context.Context, db.Tx[db.Tenant], uuid.UUID, I) (Response[O], error), opts CommandOptions,
) {
	MountResult(surfaces, spec, declaredInPlace[I](verb, summary, description, events, opts), run)
}

// declaredInPlace is the one adapter from the positional form to the declaration
// Mount reads: the same four facts named as fields, the guard and the collection
// route lifted to where a CommandSpec keeps them, and the copies in Options
// cleared so the declaration cannot be read as stating either fact twice.
func declaredInPlace[I any](verb, summary, description string, events []string, opts CommandOptions) CommandSpec[I] {
	cmd := CommandSpec[I]{
		Verb: verb, Summary: summary, Doc: description, Emits: events,
		Auth: opts.Auth, Collection: opts.Collection, Options: opts,
	}
	cmd.Options.Auth, cmd.Options.Collection = httpx.Auth{}, false
	return cmd
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
		Collection: opts.Collection, Auth: auth, MintsCredential: credential,
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
