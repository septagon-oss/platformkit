package pkit_test

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/pkit"
)

// buildWait is how long one of these boots gets to reach the point the case is asking about. Build
// migrates the database it was handed before it ever touches a transport, so this is a wall on the
// Postgres rather than on the code under test: the case refused at 15.53 s — twice, on 2026-10-06,
// inside `make check`, on a host carrying eight of this program's whole suites at once — with
// `the first build did not reach its transport`, while the same package passed in 168.435 s when it
// was asked on its own. The assertion is unchanged: a build that never gets there is still a failure,
// and a build that gets there late is still the thing this case is about.
const buildWait = 2 * time.Minute

// A concurrent boot can declare an event while another boot is opening its
// transport. Whichever composition is refused must leave its database empty.
func TestConcurrentShapeRefusalHasNoEffects(t *testing.T) {
	firstDB := onOneDatabase(t)
	t.Cleanup(func() { events.DeclareAll(nil) })
	firstAtTransport := make(chan struct{})
	continueFirst := make(chan struct{})
	var release sync.Once
	stopFirst := func() { release.Do(func() { close(continueFirst) }) }
	defer stopFirst()

	firstDeployment := buildDeployment(firstDB, app.All)
	firstDeployment.Transports.Memory = func() events.Transport {
		close(firstAtTransport)
		<-continueFirst
		return memory.New()
	}
	type buildResult struct {
		runtime *pkit.Runtime
		err     error
	}
	firstResult := make(chan buildResult, 1)
	go func() {
		runtime, err := pkit.NewApp("collect").Use(doors, desk, postedModule[postedNumber]("ledger")).
			Build(t.Context(), firstDeployment)
		firstResult <- buildResult{runtime, err}
	}()
	select {
	case <-firstAtTransport:
	case <-time.After(buildWait):
		t.Fatal("the first build did not reach its transport")
	}

	t.Run("b", func(t *testing.T) {
		secondDB := onOneDatabase(t)
		if secondDB.Database.URL == firstDB.Database.URL {
			t.Fatal("the concurrent builds did not use separate databases")
		}
		secondResult := make(chan buildResult, 1)
		go func() {
			runtime, err := pkit.NewApp("collect").Use(doors, desk, postedModule[postedText]("ledger")).
				Build(t.Context(), buildDeployment(secondDB, app.All))
			secondResult <- buildResult{runtime, err}
		}()

		// A correction may serialize these boots, so let the first proceed if
		// the second is still waiting. On the current path the second finishes
		// while the first is at its transport, after both passed CheckDeclared.
		var second buildResult
		select {
		case second = <-secondResult:
		case <-time.After(10 * time.Second):
			stopFirst()
			select {
			case second = <-secondResult:
			case <-time.After(buildWait):
				t.Fatal("the second build did not finish")
			}
		}
		stopFirst()
		first := <-firstResult
		if first.runtime != nil {
			defer first.runtime.Close()
		}
		if second.runtime != nil {
			defer second.runtime.Close()
		}
		if (first.err == nil) == (second.err == nil) {
			t.Errorf("two incompatible event shapes must have one accepted and one refused build: first=%v, second=%v", first.err, second.err)
		}
		for _, result := range []struct {
			name   string
			build  buildResult
			config string
		}{
			{"first", first, firstDB.Database.MigrateURL},
			{"second", second, secondDB.Database.MigrateURL},
		} {
			if result.build.err == nil {
				continue
			}
			if !strings.Contains(result.build.err.Error(), "ledger.posted") {
				t.Errorf("%s build was refused for another reason: %v", result.name, result.build.err)
			}
			if got := tablesIn(t, result.config); got != 0 {
				t.Errorf("%s build was refused for its event shape after migrating %d tables", result.name, got)
			}
		}
	})
}
