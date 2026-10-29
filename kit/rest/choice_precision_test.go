package rest

import (
	"context"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
)

type preciseChoice struct {
	crud.Base
	Shapes []string `json:"shapes"`
	Count  int64    `json:"count"`
}

func TestChoiceCreateOverrideReceivesOnlyNormalizedAnswers(t *testing.T) {
	s := Spec[*preciseChoice]{Choices: &entity.ChoiceFields{Fields: []entity.ChoiceField{{Name: "shapes", Choices: *formChoices()[0].Choices}}}}
	calls := 0
	s.Create = func(_ context.Context, _ db.Tx[db.Tenant], row *preciseChoice) (*preciseChoice, error) {
		calls++
		return row, nil
	}
	s.AfterCreate = func(context.Context, db.Tx[db.Tenant], *preciseChoice) error {
		t.Fatal("override invoked a generic create hook")
		return nil
	}
	var tx db.Tx[db.Tenant]
	got, err := s.CreateRow(t.Context(), tx, &preciseChoice{Shapes: []string{"round", "square", "triangle"}})
	if err != nil || calls != 1 || !slices.Equal(got.Shapes, []string{"square", "triangle"}) {
		t.Fatalf("normalized override: %+v, %d calls, %v", got, calls, err)
	}
	for _, ids := range [][]string{{"unknown", "round", "square"}, {"round", "round"}, {"round", "none"}} {
		got, err := s.CreateRow(t.Context(), tx, &preciseChoice{Shapes: ids})
		if err == nil || got != nil || calls != 1 {
			t.Fatalf("invalid answer reached owner: %+v, %d calls, %v", got, calls, err)
		}
	}
}

func (*preciseChoice) TableName() string { return "precise_choices" }

func TestChoiceNormalizationPreservesUnrelatedIntegerPrecision(t *testing.T) {
	s := Spec[*preciseChoice]{Choices: &entity.ChoiceFields{Fields: []entity.ChoiceField{{Name: "shapes", Choices: *formChoices()[0].Choices}}}}
	const count = int64(9007199254740993)
	body, err := s.choiceCreateBody([]byte(`{"shapes":["round"],"count":9007199254740993}`), &preciseChoice{})
	if err != nil || body.Count != count {
		t.Fatalf("JSON normalization: %+v %v", body, err)
	}
	row, err := s.choiceCreateRow(&preciseChoice{Shapes: []string{"round"}, Count: count})
	if err != nil || row.Count != count {
		t.Fatalf("row normalization: %+v %v", row, err)
	}
}
