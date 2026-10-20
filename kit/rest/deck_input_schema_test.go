package rest_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/rest"
)

func TestDeckRefusesPromotedArgumentsWithDistinctJSONNames(t *testing.T) {
	type Source struct {
		Value string `json:"source" validate:"required"`
	}
	type Destination struct {
		Value string `json:"destination" validate:"required"`
	}
	type input struct {
		Source
		Destination
	}
	var decoded input
	if err := json.Unmarshal([]byte(`{"source":"inbox","destination":"archive"}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Source.Value != "inbox" || decoded.Destination.Value != "archive" {
		t.Fatalf("command arguments are not reachable through JSON: %+v", decoded)
	}

	s := deckSpec()
	delete(s.Present.Deck.Directions, "left")
	api, _ := patchDocumentAPI()
	s.Mount(api.Surfaces(s.Module))
	rest.Command(api.Surfaces(s.Module), s, "publish", "Publish", "", nil,
		func(context.Context, db.Tx[db.Tenant], uuid.UUID, input) (*Task, error) { return nil, nil },
		rest.CommandOptions{})
	err := api.ValidateDeclarations()
	if err == nil {
		t.Fatal("deck accepted a command with required source and destination arguments and no defaults")
	}
	if !strings.Contains(err.Error(), "deck.directions.right.command") ||
		(!strings.Contains(err.Error(), "source") && !strings.Contains(err.Error(), "destination")) {
		t.Fatalf("deck refusal must name the omitted argument: %v", err)
	}
}
