package internal_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/decide/contracts/decidetest"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// A generic deck client knows only the catalogue: it lists the declared queue,
// then answers each card with a direction's command, filling the body from the
// declared row bindings. Across two questions the participant belongs to, that
// loop must record one ballot per card and leave the queue empty.
func TestADeckClientEmptiesTheCatalogueQueueThroughItsBoundCommands(t *testing.T) {
	w := newWorld(t)
	second := decidetest.Seed()
	second.Question.ID, second.Options = uuid.New(), second.Options[:2]
	for i := range second.Options {
		second.Options[i].ID, second.Options[i].QuestionID = uuid.New(), second.Question.ID
	}
	for i := range second.Participants {
		second.Participants[i].ID, second.Participants[i].QuestionID = uuid.New(), second.Question.ID
	}
	w.run(t, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := put(ctx, tx, decidetest.Seed()); err != nil {
			return err
		}
		if err := crud.Create(ctx, tx, &second.Question); err != nil {
			return err
		}
		for i := range 2 {
			if err := crud.Create(ctx, tx, &second.Options[i]); err != nil {
				return err
			}
			if err := crud.Create(ctx, tx, &second.Participants[i]); err != nil {
				return err
			}
		}
		return nil
	})
	api, handler := mountedDecide(t, w)
	r := api.Resources()[1]
	entry := screens.Describe1(r, true)
	if entry.Presentation == nil || entry.Presentation.Deck == nil {
		t.Fatalf("%s/%s declares no deck", r.Module, r.Entity)
	}
	deck, query := entry.Presentation.Deck, url.Values{"filter": entry.Presentation.Deck.Queue, "sort": {entry.Presentation.Deck.Order}}
	list := "/api/v1/" + r.Module + r.Path + "?" + query.Encode()
	commands := map[string]screens.Command{}
	for _, c := range entry.Commands {
		commands[c.Verb] = c
	}
	directions, cast := []string{"right", "left", "up", "down", "right"}, 0
	for ; cast <= len(directions); cast++ {
		resp := request(t, handler, http.MethodGet, list, "")
		var page struct {
			Items []map[string]any `json:"items"`
			Total int64            `json:"total"`
		}
		if err := json.Unmarshal(resp.Body.Bytes(), &page); err != nil || resp.Code != http.StatusOK {
			t.Fatalf("GET %s: %d %s %v", list, resp.Code, resp.Body.String(), err)
		}
		if page.Total == 0 || cast == len(directions) {
			if page.Total != 0 || len(page.Items) != 0 {
				t.Fatalf("after %d answered cards the queue still holds %+v", cast, page)
			}
			break
		}
		command, ok := commands[deck.Directions[directions[cast]].Command]
		if !ok || command.Path != "" || command.Collection {
			t.Fatalf("direction %s names no row command on the card's own path: %+v", directions[cast], command)
		}
		body := map[string]any{}
		for field, from := range command.Bindings {
			body[field] = page.Items[0][from]
		}
		raw, _ := json.Marshal(body)
		path := "/api/v1/" + r.Module + r.Path + "/" + page.Items[0]["id"].(string) + "/" + command.Verb
		if answer := request(t, handler, http.MethodPost, path, string(raw)); answer.Code != http.StatusOK {
			t.Fatalf("POST %s %s: %d %s", path, raw, answer.Code, answer.Body.String())
		}
	}
	var ballots, questions int
	var values string
	if err := w.admin.QueryRowContext(t.Context(), `SELECT count(*), count(DISTINCT question_id), string_agg(value, ',' ORDER BY value)
		FROM decide_votes WHERE person_id = $1 AND value IS NOT NULL`, decidetest.Alice).Scan(&ballots, &questions, &values); err != nil {
		t.Fatal(err)
	}
	if ballots != len(directions) || questions != 2 || strings.Count(values, "yes") != 2 || !strings.Contains(values, "strong") || !strings.Contains(values, "skip") {
		t.Fatalf("ballots %d over %d questions (%s); want %d over 2, one per answered card", ballots, questions, values, len(directions))
	}
}
