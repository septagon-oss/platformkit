package filetest

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

func storageConcurrentCreation(t *testing.T, fresh func(*testing.T) StorageFixture) {
	fixture, key := fresh(t), contracts.Key(uuid.NewString())
	const writers = 8
	start, successes := make(chan struct{}), make(chan string, writers)
	var group sync.WaitGroup
	for i := range writers {
		group.Go(func() {
			body := strings.Repeat(fmt.Sprintf("writer %d\n", i), 128)
			<-start
			if err := fixture.Storage.Put(t.Context(), fixture.Scope, key, strings.NewReader(body), int64(len(body)), contracts.Meta{}); err == nil {
				successes <- body
			}
		})
	}
	close(start)
	group.Wait()
	close(successes)
	var winners []string
	for body := range successes {
		winners = append(winners, body)
	}
	if len(winners) != 1 {
		t.Fatalf("%d successful creates, want exactly one", len(winners))
	}
	if got := storageRead(t, fixture.Storage, fixture.Scope, key); got != winners[0] {
		t.Fatal("stored bytes are not the winning writer's complete stream")
	}
}
