package authtest_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/user/contracts/usertest"
)

func TestFakeImplementsPasskeyCeremonies(t *testing.T) {
	fake := authtest.NewFake(usertest.NewFake())
	if _, ok := any(fake).(contracts.Passkeys); !ok {
		t.Fatal("the auth fake does not implement the Passkeys contract")
	}
}
