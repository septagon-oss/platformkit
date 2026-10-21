package pkit_test

// Only a read pkit could not answer is turned into a module's problem. A panic
// of the module's own — an error, its own nil dereference, a string — reaches
// the caller as the identical value, so a programmer mistake is never reported
// as a wiring fault.

import (
	"errors"
	"runtime"
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
)

var errProgrammer = errors.New("own: a programmer mistake")

type counter struct{ n int }

func nilCounter() *counter { return nil }

func TestAPanicThatIsNotAFailedReadReachesTheCallerUnchanged(t *testing.T) {
	for _, tc := range []struct {
		what  string
		build func(*pkit.Wiring) (module.Module, error)
		same  func(r any) bool
	}{
		{"an error", func(*pkit.Wiring) (module.Module, error) { panic(errProgrammer) },
			func(r any) bool { return r == errProgrammer }},
		{"the build's own nil dereference", func(*pkit.Wiring) (module.Module, error) {
			return module.Module{Name: "own"}, errors.New(string(rune(nilCounter().n)))
		}, func(r any) bool { _, ok := r.(runtime.Error); return ok }},
		{"a string", func(*pkit.Wiring) (module.Module, error) { panic("a string") },
			func(r any) bool { return r == "a string" }},
	} {
		app := pkit.NewApp("collect").Use(pkit.NewModule("own", tc.build))
		returned := false
		recovered := func() (r any) {
			defer func() { r = recover() }()
			_ = app.Validate(dev)
			returned = true
			return nil
		}()
		if returned || !tc.same(recovered) {
			t.Errorf("%s: Validate returned %v and recovered %v, want the panic raised again unchanged", tc.what, returned, recovered)
		}
	}
}
