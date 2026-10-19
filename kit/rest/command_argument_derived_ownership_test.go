package rest_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/rest"
)

func TestCommandOwnershipFollowsPromotedAndShadowedFields(t *testing.T) {
	type counter uint64
	type observed struct {
		Value *counter `json:"revision"`
	}
	type stored struct {
		crud.Base
	}
	t.Run("promoted nullable named integer", func(t *testing.T) {
		refuseCommandArgument[struct{ observed }](t, rest.CommandOptions{}, "revision")
	})
	t.Run("promoted Base", func(t *testing.T) {
		refuseCommandArgument[struct{ stored }](t, rest.CommandOptions{}, "id")
	})
	t.Run("shadowing id leaves server timestamps", func(t *testing.T) {
		refuseCommandArgument[struct {
			crud.Base
			ID string `json:"id"`
		}](t, rest.CommandOptions{}, "createdAt")
	})
	t.Run("shadowing every visible Base field makes them caller owned", func(t *testing.T) {
		admitCommandArgument[struct {
			crud.Base
			ID        uuid.UUID `json:"id"`
			CreatedAt time.Time `json:"createdAt"`
			UpdatedAt time.Time `json:"updatedAt"`
		}](t, "id", "createdAt", "updatedAt")
	})
	t.Run("Go name without JSON rename", func(t *testing.T) {
		admitCommandArgument[struct{ Revision int64 }](t, "Revision")
	})
	t.Run("list is not an integer counter", func(t *testing.T) {
		admitCommandArgument[struct {
			Revisions []int64 `json:"revision"`
		}](t, "revision")
	})
}
