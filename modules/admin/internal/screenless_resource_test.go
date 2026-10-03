package internal

// A Spec that
// offers no list leaves httpx.Resource.Screen empty, so no generated page exists
// at any address. ownScreens answers "which resources serve their own workspace
// pages" by asking whether a recorded path is the resource's screen or sits
// beneath it; beside an empty screen the prefix is "/", and every recorded path
// starts with it. The answer is then "it serves its own pages", which modules/
// admin logs at boot as a fact about a module that serves nothing.

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
)

func TestAResourceWithNoScreenAddressOwnsNoPages(t *testing.T) {
	screenless := httpx.Resource{Module: "task", Entity: "task"}
	own := ownScreens([]string{"/api/v1/task/task", "/app/task/tasks"}, []httpx.Resource{screenless})
	if own[screenless.Screen] {
		t.Errorf("a resource with no screen address is reported as serving its own workspace pages (own[%q])", screenless.Screen)
	}
}
