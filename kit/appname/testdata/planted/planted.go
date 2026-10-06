// planted.go is the census's own test case: a file that forms every shared name
// inline, the way kit/appname exists to stop. The go tool ignores anything under
// testdata, so nothing compiles this; the census scan reads it and has to report
// all eight names. A census that reports nothing here is a census whose patterns
// match nothing, and TestTheCensusFindsAPlantedName says so instead of passing.
package planted

import (
	"path/filepath"
	"strings"

	"github.com/nats-io/nats.go"
)

const SubjectPrefix = "platformkit"

const stream = "PLATFORMKIT"

// local is the storage adapter's shape, so its path call reads like the real one.
type local struct{ dir string }

type tenant struct{ s string }

func (t tenant) String() string { return t.s }

// cookieName is the session cookie, spelled by hand.
func cookieName(base string, secure bool) string {
	if secure {
		return "__Host-" + base
	}
	return base
}

// lockKey is the job's advisory lock.
func lockKey(job string) string { return "job:" + job }

// subject is the event's address.
func subject(name string) string { return SubjectPrefix + "." + name }

// connection names the process on the broker.
func connection() nats.Option { return nats.Name("platformkit") }

// durable names the subscription.
func durable(Module, name string) string {
	return Module + "-" + strings.ReplaceAll(name, ".", "-")
}

// limitKey is a rate-limit bucket: the tenant's id, then the caller's key.
func limitKey(t tenant, key string) string { return t.String() + "/" + key }

// fileAt is where a stored file sits on the volume.
func (l local) fileAt(k string) string { return filepath.Join(l.dir, k[:2], k) }
