package screens_test

// review_r1_operations_entry_test.go — review 1's pin on the one wire key
// T-0185 added. `operations` is printed only by Describe1, and SPECIFY §4 named
// the case that proves it (TestDescribePublishesOnlyTheOperationsTheResource
// Offers); no such case exists in the tree, so the key the native shell is told
// to read has never been published in a test. The second case asks whether the
// entry the same function writes contradicts itself: an entry that names no
// write verb and carries no command still answers `writable` of the caller's
// permission alone.

import (
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// TestReviewR1TheEntryNamesTheVerbsItsRouterMounted is the published key: both
// verbs for a resource that mounted two, and no key at all for one that mounted
// five, because an absent key already means all five.
func TestReviewR1TheEntryNamesTheVerbsItsRouterMounted(t *testing.T) {
	only := resource()
	only.Operations = []httpx.CRUD{httpx.CRUDList, httpx.CRUDRead}
	if got := screens.Describe1(only, false).Operations; !slices.Equal(got, []string{"list", "read"}) {
		t.Errorf("the entry of a {list,read} resource publishes operations %v", got)
	}
	if got := screens.Describe1(resource(), false).Operations; got != nil {
		t.Errorf("an all-five entry publishes operations %v, want the absent key", got)
	}
}

// TestReviewR1AnEntryThatMountsNoWriteIsNotWritable: `writable` is the key the
// shell that has not read `operations` still obeys, and for this resource there
// is nothing it may write — no create, no patch, no delete, no command.
func TestReviewR1AnEntryThatMountsNoWriteIsNotWritable(t *testing.T) {
	only := resource()
	only.Operations = []httpx.CRUD{httpx.CRUDList, httpx.CRUDRead}
	if e := screens.Describe1(only, true); e.Writable {
		t.Errorf("operations %v name no write verb and the resource carries no command, yet the entry says writable=true", e.Operations)
	}
}
