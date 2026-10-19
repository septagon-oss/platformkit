package app

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestAHeartbeatChecksTheRefreshedPrincipalsRoles(t *testing.T) {
	_, identity, lines := openStreamRoute(t, 10*time.Millisecond)
	identity.revoked.Store(true)
	deadline := time.After(time.Second)
	dropped := false
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatal("stream ended instead of dropping the revoked topic and keeping the principal topic")
			}
			if line == "event: dropped" {
				dropped = true
			}
			if data, ok := strings.CutPrefix(line, "data: "); ok && dropped {
				var notice map[string]string
				if err := json.Unmarshal([]byte(data), &notice); err != nil {
					t.Fatal(err)
				}
				if notice["topic"] != "task.closed" || notice["reason"] != "permission" {
					t.Fatalf("heartbeat dropped the wrong topic or reason: %v", notice)
				}
				if !identity.refreshed.Load() {
					t.Fatal("topic was dropped without refreshing the principal")
				}
				return
			}
		case <-deadline:
			if !identity.refreshed.Load() {
				t.Fatal("heartbeat never reached authentication after the roles changed")
			}
			t.Fatal("heartbeat never dropped task.closed after authentication returned a principal without the reader role")
		}
	}
}
