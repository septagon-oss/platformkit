package pkit_test

// kit/app documents one Start per App, and pkit refuses a second Build of one
// App rather than attempt it. The refusal holds when the two Builds arrive at
// once: exactly one of them starts a lifecycle.

import (
	"sync"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestTwoBuildsOfOneAppAtOnceStartOneLifecycle(t *testing.T) {
	cfg := onOneDatabase(t)
	a := pkit.NewApp("collect").Use(doors, desk)

	var wg sync.WaitGroup
	var mu sync.Mutex
	var started []*pkit.Runtime
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rt, err := a.Build(t.Context(), buildDeployment(cfg, app.All))
			if err == nil {
				mu.Lock()
				started = append(started, rt)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	for _, rt := range started {
		defer rt.Close()
	}
	if len(started) != 1 {
		t.Errorf("two Builds of one App at once started %d lifecycles; one App is one lifecycle", len(started))
	}
}
