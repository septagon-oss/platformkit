package internal

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/richtext"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/decide/contracts"
)

func classified(err error) error {
	switch {
	case errors.Is(err, contracts.ErrNotFound):
		return errors.Join(crud.ErrNotFound, err)
	case errors.Is(err, contracts.ErrInvalid), errors.Is(err, richtext.ErrMissing):
		return errors.Join(crud.ErrInvalid, err)
	case errors.Is(err, contracts.ErrDenied), errors.Is(err, contracts.ErrInvite), errors.Is(err, contracts.ErrUndo):
		return errors.Join(tenancy.ErrPolicyDenied, err)
	case errors.Is(err, contracts.ErrClosed), errors.Is(err, contracts.ErrStale), errors.Is(err, contracts.ErrLastOption),
		errors.Is(err, contracts.ErrJoinDisabled), errors.Is(err, contracts.ErrExhausted):
		return errors.Join(crud.ErrConflict, err)
	default:
		return err
	}
}

// RevisionBody carries the caller's observed question revision.
type RevisionBody struct {
	IfRevision int64 `json:"ifRevision" minimum:"1"`
}
type questionPath struct {
	ID uuid.UUID `path:"id" format:"uuid"`
}
type operationBody[T any] struct{ Body T }
type questionBody[T any] struct {
	questionPath
	Body T
}

// Routes mounts service-backed resources and typed projections. Generic CRUD
// mutations are absent; every command uses the same authoritative service.
func (s *Service) Routes(surfaces httpx.Surfaces) {
	questions, cards := s.specs()
	questions.Mount(surfaces)
	cards.Mount(surfaces)
	manage := httpx.Permission(contracts.PermissionManage).RecheckingReplay(s.replayAccess(true))
	vote := httpx.Permission(contracts.PermissionVote).RecheckingReplay(s.replayAccess(false))
	read := httpx.Permission(contracts.PermissionRead)
	rest.CommandResult(surfaces, questions, "close", "Close question", "Finish voting on this question.", []string{contracts.EventClosed},
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in RevisionBody) (rest.Response[contracts.Question], error) {
			out, err := s.Close(ctx, tx, contracts.QuestionInput{QuestionID: id, IfRevision: in.IfRevision})
			return rest.Response[contracts.Question]{Result: out}, classified(err)
		}, rest.CommandOptions{Auth: manage, Present: entity.CommandHints{Label: "Close question"}})
	rest.CommandResult(surfaces, questions, "remove-option", "Remove option", "Keep at least one option available.", nil,
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in struct {
			RevisionBody
			OptionID uuid.UUID `json:"optionId" format:"uuid"`
		}) (rest.Response[contracts.Question], error) {
			out, err := s.RemoveOption(ctx, tx, contracts.RemoveOptionInput{QuestionInput: contracts.QuestionInput{QuestionID: id, IfRevision: in.IfRevision}, OptionID: in.OptionID})
			return rest.Response[contracts.Question]{Result: out}, classified(err)
		}, rest.CommandOptions{Auth: manage, Present: entity.CommandHints{Label: "Remove option"}})
	rest.CommandResult(surfaces, questions, "add-participant", "Add participant", "Invite an existing workspace person.", nil,
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in struct {
			RevisionBody
			PersonID uuid.UUID `json:"personId" format:"uuid"`
		}) (rest.Response[contracts.ParticipantChange], error) {
			out, err := s.AddParticipant(ctx, tx, contracts.AddParticipantInput{QuestionInput: contracts.QuestionInput{QuestionID: id, IfRevision: in.IfRevision}, PersonID: in.PersonID})
			return rest.Response[contracts.ParticipantChange]{Result: out}, classified(err)
		}, rest.CommandOptions{Auth: manage, Present: entity.CommandHints{Label: "Add participant"}})
	rest.CommandResult(surfaces, questions, "issue-invite", "Create invite link", "Replace any previous invitation.", nil,
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in struct {
			RevisionBody
			ExpiresAt time.Time `json:"expiresAt"`
		}) (rest.Response[contracts.InviteReply], error) {
			out, err := s.IssueInvite(ctx, tx, contracts.IssueInviteInput{QuestionInput: contracts.QuestionInput{QuestionID: id, IfRevision: in.IfRevision}, ExpiresAt: in.ExpiresAt})
			return rest.Response[contracts.InviteReply]{Result: out}, classified(err)
		}, rest.CommandOptions{Auth: manage, MintsCredential: true, Present: entity.CommandHints{Label: "Create invite link"}})
	rest.CommandResult(surfaces, questions, "revoke-invite", "Revoke invite link", "Stop further admission through the current link.", nil,
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in RevisionBody) (rest.Response[contracts.Question], error) {
			out, err := s.RevokeInvite(ctx, tx, contracts.QuestionInput{QuestionID: id, IfRevision: in.IfRevision})
			return rest.Response[contracts.Question]{Result: out}, classified(err)
		}, rest.CommandOptions{Auth: manage, Present: entity.CommandHints{Label: "Revoke invite link"}})
	rest.CommandResult(surfaces, questions, "join", "Join question", "Use an invitation after signing in.", nil,
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in struct {
			Token      string `json:"token"`
			IfRevision int64  `json:"ifRevision"`
		}) (rest.Response[contracts.ParticipantChange], error) {
			out, err := s.Join(ctx, tx, contracts.JoinInput{QuestionID: id, Token: in.Token, IfRevision: in.IfRevision})
			return rest.Response[contracts.ParticipantChange]{Result: out}, classified(err)
		}, rest.CommandOptions{Auth: vote, Present: entity.CommandHints{Label: "Join question"}})
	for _, value := range []contracts.Value{contracts.Yes, contracts.No, contracts.Strong, contracts.Skip} {
		rest.CommandResult(surfaces, cards, string(value), "Vote "+string(value), "Record your choice while voting is open.", []string{contracts.EventVoted, contracts.EventMatched},
			func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in struct {
				QuestionID uuid.UUID `json:"questionId" format:"uuid"`
				IfRevision int64     `json:"ifRevision" minimum:"0"`
			}) (rest.Response[contracts.Vote], error) {
				out, err := s.Vote(ctx, tx, contracts.VoteInput{QuestionID: in.QuestionID, OptionID: id, IfRevision: in.IfRevision, Value: value})
				return rest.Response[contracts.Vote]{Result: out.Vote, UndoToken: out.UndoToken}, classified(err)
			}, rest.CommandOptions{Auth: vote, Undo: "undo-vote", Bindings: &rest.RowBindings{Fields: map[string]string{"questionId": "questionId", "ifRevision": "voteRevision"}}, Present: entity.CommandHints{Label: "Vote " + string(value)}})
	}
	rest.CommandResult(surfaces, cards, "undo-vote", "Undo vote", "Restore the choice before your last vote.", []string{contracts.EventVoted, contracts.EventMatched},
		func(ctx context.Context, tx db.Tx[db.Tenant], _ uuid.UUID, in contracts.UndoVoteInput) (rest.Response[contracts.Vote], error) {
			out, err := s.UndoVote(ctx, tx, in)
			return rest.Response[contracts.Vote]{Result: out.Vote}, classified(err)
		}, rest.CommandOptions{Auth: vote, Collection: true, Present: entity.CommandHints{Label: "Undo vote"}})
	op := func(verb, method, path string, emitted []string) huma.Operation {
		out := huma.Operation{OperationID: "decide-" + verb, Method: method, Path: path, Tags: []string{"decide"}}
		if len(emitted) > 0 {
			out.Extensions = map[string]any{httpx.EventsExtension: emitted}
		}
		return out
	}
	private := rest.OperationOptions{CacheControl: "private, no-store"}
	rest.Operation(surfaces.App, op("open", http.MethodPost, "/questions/open", nil), manage,
		func(ctx context.Context, tx db.Tx[db.Tenant], _ uuid.UUID, in *operationBody[contracts.OpenInput]) (contracts.Question, error) {
			out, err := s.Open(ctx, tx, in.Body)
			return out, classified(err)
		}, private)
	rest.Operation(surfaces.App, op("add-option", http.MethodPost, "/questions/{id}/add-option", nil), manage,
		func(ctx context.Context, tx db.Tx[db.Tenant], _ uuid.UUID, in *questionBody[struct {
			RevisionBody
			Option contracts.OptionInput `json:"option"`
		}]) (contracts.OptionChange, error) {
			out, err := s.AddOption(ctx, tx, contracts.AddOptionInput{QuestionInput: contracts.QuestionInput{QuestionID: in.ID, IfRevision: in.Body.IfRevision}, Option: in.Body.Option})
			return out, classified(err)
		}, private)
	readOperation(surfaces, op("results", http.MethodGet, "/questions/{id}/results", nil), read, s.Results)
	readOperation(surfaces, op("deck", http.MethodGet, "/questions/{id}/deck", nil), vote, s.Deck)
	readOperation(surfaces, op("options", http.MethodGet, "/questions/{id}/options", nil), read, s.Options)
	readOperation(surfaces, op("participants", http.MethodGet, "/questions/{id}/participants", nil), read, s.Participants)
	readOperation(surfaces, op("export", http.MethodGet, "/questions/{id}/export", nil), read, s.Export)
}

// replayAccess holds current grants without taking an aggregate lock: recovery
// runs no command and creator/participant identities cannot change in this version.
func (s *Service) replayAccess(manage bool) httpx.ReplayCheck {
	return func(ctx context.Context, tx db.Tx[db.Tenant]) (bool, error) {
		if err := scope(ctx, tx); err != nil {
			return false, err
		}
		actor, err := s.access.Current(ctx, tx, nil)
		if errors.Is(err, contracts.ErrDenied) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		principal, _ := tenancy.PrincipalFrom(ctx)
		return actor.PersonID != uuid.Nil && actor.PersonID == principal.UserID && actor.Read &&
			((manage && actor.Manage) || (!manage && actor.Vote)), nil
	}
}

func readOperation[O any](surfaces httpx.Surfaces, op huma.Operation, auth httpx.Auth, run func(context.Context, db.Tx[db.Tenant], uuid.UUID) (O, error)) {
	rest.Operation(surfaces.App, op, auth, func(ctx context.Context, tx db.Tx[db.Tenant], _ uuid.UUID, in *questionPath) (O, error) {
		out, err := run(ctx, tx, in.ID)
		return out, classified(err)
	}, rest.OperationOptions{CacheControl: "private, no-store"})
}

func (s *Service) specs() (rest.Spec[*contracts.Question], rest.Spec[*contracts.Card]) {
	questions := rest.Spec[*contracts.Question]{Name: rest.Name{One: "Question", Other: "Questions"}, Module: "decide", Entity: "question", Path: "/questions", Read: contracts.PermissionRead, Write: contracts.PermissionManage,
		Operations: []httpx.CRUD{httpx.CRUDList, httpx.CRUDRead}, Present: entity.EntryHints{PrimaryField: "title", Sortable: []string{"createdAt"}},
		Source: &rest.ReadSource[*contracts.Question]{Get: func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.Question, error) {
			out, err := s.Question(ctx, tx, id)
			if err != nil {
				return nil, classified(err)
			}
			return &out, nil
		}, List: func(ctx context.Context, tx db.Tx[db.Tenant], q crud.Query) ([]*contracts.Question, int64, error) {
			if (q.Sort != "" && q.Sort != "-createdAt") || len(q.Filter) > 1 {
				return nil, 0, classified(contracts.ErrInvalid)
			}
			status := ""
			for key, value := range q.Filter {
				var ok bool
				status, ok = value.(string)
				if key != "status" || !ok {
					return nil, 0, classified(contracts.ErrInvalid)
				}
			}
			page, err := s.Questions(ctx, tx, contracts.QuestionQuery{Status: status, Offset: q.Offset, Limit: q.Limit})
			if err != nil {
				return nil, 0, classified(err)
			}
			out := make([]*contracts.Question, len(page.Items))
			for i := range page.Items {
				out[i] = &page.Items[i]
			}
			return out, page.Total, nil
		}}}
	cards := rest.Spec[*contracts.Card]{Name: rest.Name{One: "Card", Other: "Cards"}, Module: "decide", Entity: "card", Path: "/cards", Read: contracts.PermissionVote, Write: contracts.PermissionVote,
		Operations: []httpx.CRUD{httpx.CRUDList, httpx.CRUDRead}, RichTextFiles: s.images,
		Present: entity.EntryHints{PrimaryField: "title", SummaryFields: []string{"note", "link"}, CoverField: "imageId", Sortable: []string{"position"},
			Deck: &entity.DeckHints{Queue: []string{"unvoted:true"}, Order: "position", Undoable: true, Directions: map[string]entity.DeckDirection{
				"right": {Command: "yes", Label: "Yes", Tone: "success", Icon: "task"}, "left": {Command: "no", Label: "No", Tone: "danger", Icon: "box"},
				"up": {Command: "strong", Label: "Strong yes", Tone: "success", Icon: "tag"}, "down": {Command: "skip", Label: "Skip", Tone: "neutral", Icon: "calendar"}}}},
		Source: &rest.ReadSource[*contracts.Card]{Get: s.card, List: s.cards}}
	return questions, cards
}

func (s *Service) card(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.Card, error) {
	if err := scope(ctx, tx); err != nil {
		return nil, err
	}
	o, err := crud.Get[*contracts.Option](tx, id)
	if err != nil {
		return nil, classified(absent(err))
	}
	snap, actor, err := s.locked(ctx, tx, o.QuestionID, false)
	if err != nil {
		return nil, classified(err)
	}
	if _, err := contracts.ReadDeck(snap, actor, s.now()); err != nil {
		return nil, classified(err)
	}
	for _, option := range snap.Options {
		if option.ID == id && option.RemovedAt == nil {
			out := &contracts.Card{Option: option, Unvoted: true}
			for _, vote := range snap.Votes {
				if vote.OptionID == id && vote.PersonID == actor.PersonID {
					out.VoteRevision, out.Unvoted = vote.Revision, vote.Value == nil
				}
			}
			return out, nil
		}
	}
	return nil, classified(contracts.ErrNotFound)
}

func (s *Service) cards(ctx context.Context, tx db.Tx[db.Tenant], q crud.Query) ([]*contracts.Card, int64, error) {
	if (q.Sort != "" && q.Sort != "position") || q.Offset < 0 || q.Limit < 0 || q.Limit > 200 {
		return nil, 0, classified(contracts.ErrInvalid)
	}
	var question uuid.UUID
	for key, value := range q.Filter {
		switch key {
		case "questionId":
			switch typed := value.(type) {
			case uuid.UUID:
				question = typed
			case string:
				var err error
				question, err = uuid.Parse(typed)
				if err != nil {
					return nil, 0, classified(contracts.ErrInvalid)
				}
			default:
				return nil, 0, classified(contracts.ErrInvalid)
			}
			if question == uuid.Nil {
				return nil, 0, classified(contracts.ErrInvalid)
			}
		case "unvoted":
			if value != true {
				return nil, 0, classified(contracts.ErrInvalid)
			}
		default:
			return nil, 0, classified(contracts.ErrInvalid)
		}
	}
	if question == uuid.Nil {
		cards, total, err := s.cardQueue(ctx, tx, q.Offset, q.Limit)
		return cards, total, classified(err)
	}
	deck, err := s.Deck(ctx, tx, question)
	if err != nil {
		return nil, 0, classified(err)
	}
	if q.Limit == 0 {
		q.Limit = 50
	}
	start := min(q.Offset, len(deck.Cards))
	end := start + min(q.Limit, len(deck.Cards)-start)
	out := make([]*contracts.Card, 0, end-start)
	for i := start; i < end; i++ {
		out = append(out, &deck.Cards[i])
	}
	return out, deck.Remaining, nil
}
