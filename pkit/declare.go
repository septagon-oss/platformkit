// Package pkit composes an application in sentences: an app is a name and the
// modules it uses, and the modules wire themselves by the contracts they
// declare (decision 0074).
//
// Every method records; Build answers, with every problem at once, each naming
// the method that caused it. The wiring is resolved at boot, not by the
// compiler: generics check each declaration's type, and the resolver checks the
// graph — a missing or ambiguous provider, a cycle, a phase violation — before
// anything has an effect. The composition is still a list somebody wrote down:
// nothing is found that Use does not name.
//
// Build is where the sentences are answered. Its validate phase runs in a fixed
// order — the recorded settings, the resolver, every module's dry build and the
// manifest gates over what that build produced, the ports the kernel cannot
// answer for itself, the roles the app named — and every problem comes back at
// once, each naming the method that caused it, before a connection is opened, a
// migration runs or a port is listened on. Run is Build and whichever half the
// role names. A Server adds the process around an app: its configuration, the
// environment the composition is read in, the role it runs, and Host(app,
// Tenant(name, host), …), which says where an application is reached without
// deciding which host is which tenant — that answer stays the tenant module's
// rows, read per request. Explain writes what the composition answers, so a
// committed composition file and a running application cannot disagree.
//
// Boot time, honestly named: the wiring resolves when Build runs. Not when the
// sentences are written, and not once per request. A composition that does not
// resolve is refused rather than half-built, so a running application has no
// wiring left to discover and nothing here goes looking for any.
//
// A contract is keyed by its Go type. reflect is used for that type's identity
// and its printed name only, never to discover, construct or call anything.
package pkit

import (
	"reflect"
	"strings"

	"github.com/septagon-oss/platformkit/kit/module"
)

// Module is a kernel module as the resolver sees it: its name, what it
// declares, and how to build it once what it needs exists.
type Module struct {
	name  string
	decls []Declaration
	build func(*Wiring) (module.Module, error)
}

// NewModule declares a module. build runs once, after every module it needs.
func NewModule(name string, build func(*Wiring) (module.Module, error), decls ...Declaration) *Module {
	return &Module{name: name, decls: decls, build: build}
}

// Name is the module's name, as its manifest and every sentence say it.
func (m *Module) Name() string { return m.name }

type kind int

const (
	provides kind = iota
	needs
	optional
	contributes
	after
	fromDeployment
	describes
)

// Declaration is one thing a module says about itself, made only by the
// functions below.
type Declaration struct {
	kind  kind
	key   reflect.Type
	many  bool
	impls []Implementation
	about any
}

// Provides says the module is the one T of the app, unless the app chooses another.
func Provides[T any]() Declaration { return Declaration{kind: provides, key: reflect.TypeFor[T]()} }

// Needs says the module cannot be built without T, and is built after it. A
// slice, Needs[[]E], takes every contribution of E, zero or more.
func Needs[T any]() Declaration { return need[T](needs) }

// Optional says the module uses T only if the app composes it.
func Optional[T any]() Declaration { return need[T](optional) }

func need[T any](k kind) Declaration {
	t := reflect.TypeFor[T]()
	if t.Kind() == reflect.Slice {
		return Declaration{kind: k, key: t.Elem(), many: true}
	}
	return Declaration{kind: k, key: t}
}

// Contributes says the module adds one T, to whichever module takes T; that
// module is built after every contributor.
func Contributes[T any]() Declaration {
	return Declaration{kind: contributes, key: reflect.TypeFor[T]()}
}

type phase struct{}

// Everything is every other module of the app.
var Everything = phase{}

// After(Everything) says the module is built last and reads every other
// module's manifest. One module of an app may say so, and nothing may need it
// or take what it contributes: both would be built after the module that runs
// after everything. What it needs, the resolver supplies as it supplies any
// other module's need — it is built last, so its suppliers are already there.
func After(phase) Declaration { return Declaration{kind: after} }

// Implementation is one way a module may be built, picked by the deployment:
// the inputs it reads, and whether it only simulates the real thing.
type Implementation struct {
	Name      string
	Inputs    []string
	Simulated bool
}

// FromDeployment says the deployment picks one of these implementations. A
// simulated one is refused outside development.
func FromDeployment(impls ...Implementation) Declaration {
	return Declaration{kind: fromDeployment, impls: impls}
}

// Describes attaches a value read without building the module, such as the
// examples of a design system.
func Describes(about any) Declaration { return Declaration{kind: describes, about: about} }

// contract is the printed name of a contract: cartcontracts.Service for a type
// declared in …/cart/contracts.
func contract(t reflect.Type) string {
	if t.Kind() == reflect.Pointer {
		return "*" + contract(t.Elem())
	}
	if t.Name() == "" || t.PkgPath() == "" {
		return t.String()
	}
	dir, pkg := owner(t)
	if pkg == "contracts" && dir != "" {
		return dir + "contracts." + t.Name()
	}
	return pkg + "." + t.Name()
}

// owner reads the module that owns a contract off its path, …/cart/contracts:
// house rule 3's shape, not a registry.
func owner(t reflect.Type) (dir, pkg string) {
	parts := strings.Split(t.PkgPath(), "/")
	pkg = parts[len(parts)-1]
	if len(parts) > 1 {
		dir = parts[len(parts)-2]
	}
	return dir, pkg
}
