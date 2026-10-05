package jobs

// The brief's acceptance list asks for one sentence about the scheduler that no case
// in this repository said: two apps over one database, each running a job the same
// module named the same way, and both of them running. The advisory lock is the
// mechanism — db.TryLock on a name — and while the lock's name was the bare job name
// the two apps took turns: whichever process held job:purge silenced every other
// app's purge on every replica. appname.JobLock puts the slug in the name, which the
// unit case in kit/appname checks as two strings that differ. Strings are not
// schedules: this runs the two schedulers against the one database and counts.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestTwoAppsOfOneDatabaseBothRunTheirSameNamedJob(t *testing.T) {
	_, conn := dbtest.Schema(t)
	name := unique("purge")

	var mu sync.Mutex
	running := map[string]int{}

	// App acme gets in first and holds its lock for as long as the case needs the
	// two apps to be live at once; app acme-billing is asked to run its own job of
	// the same name while that hold is up.
	acmeStarted := make(chan struct{})
	release := make(chan struct{})
	jobOf := func(app string) Job {
		return Job{Name: name, Every: time.Hour, Run: func(context.Context, *db.Conn) error {
			mu.Lock()
			running[app]++
			mu.Unlock()
			if app == "acme" {
				close(acmeStarted)
				<-release
			}
			return nil
		}}
	}
	jobAcme, jobBilling := jobOf("acme"), jobOf("acme-billing")

	acme := NewScheduler(conn, quiet(), appname.MustParse("acme"), jobAcme)
	billing := NewScheduler(conn, quiet(), appname.MustParse("acme-billing"), jobBilling)

	done := make(chan struct{})
	go func() {
		defer close(done)
		acme.run(t.Context(), jobAcme)
	}()
	<-acmeStarted

	billing.run(t.Context(), jobBilling)

	mu.Lock()
	billingRuns := running["acme-billing"]
	acmeRuns := running["acme"]
	mu.Unlock()
	if billingRuns != 1 {
		t.Errorf("app acme-billing ran %q %d times while app acme held its own lock of the same name, want 1: one advisory lock is one job, and the lock has to carry the app", name, billingRuns)
	}
	if acmeRuns != 1 {
		t.Errorf("app acme ran %q %d times, want 1", name, acmeRuns)
	}

	// A second replica of the app that holds the lock runs nothing while it is held:
	// the app segment scopes the lock, it does not remove it. Two apps running in
	// parallel is the point; two replicas of one app running the same tick is not.
	acmeAgain := NewScheduler(conn, quiet(), appname.MustParse("acme"), jobAcme)
	acmeAgain.run(t.Context(), jobAcme)
	mu.Lock()
	held := running["acme"]
	mu.Unlock()
	if held != 1 {
		t.Errorf("a second replica of app acme ran %q %d times while the first held the lock, want 1", name, held)
	}

	close(release)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("app acme's job never returned once its lock was released")
	}
}
