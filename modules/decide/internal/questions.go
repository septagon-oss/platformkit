package internal

import (
	"context"
	"math"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/richtext"
	"github.com/septagon-oss/platformkit/modules/decide/contracts"
)

func text(value string, limit int, multiline bool) bool {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > limit {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) && !(multiline && (r == '\t' || r == '\n')) {
			return false
		}
	}
	return true
}

func optionInput(in contracts.OptionInput) (contracts.OptionInput, error) {
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" || !text(in.Title, 200, false) || !text(in.Note, 2000, true) || in.Position < 0 ||
		(in.ImageID != nil && *in.ImageID == uuid.Nil) || !text(in.Link, 2048, false) || len(in.Link) > 2048 {
		return contracts.OptionInput{}, contracts.ErrInvalid
	}
	if in.Link != "" {
		u, err := url.Parse(in.Link)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil {
			return contracts.OptionInput{}, contracts.ErrInvalid
		}
	}
	return in, nil
}

func (s *Service) Open(ctx context.Context, tx db.Tx[db.Tenant], in contracts.OpenInput) (contracts.Question, error) {
	if err := scope(ctx, tx); err != nil {
		return contracts.Question{}, err
	}
	actor, err := s.access.Current(ctx, tx, nil)
	if err != nil {
		return contracts.Question{}, err
	}
	if !actor.Read || !actor.Manage || actor.PersonID == uuid.Nil {
		return contracts.Question{}, contracts.ErrDenied
	}
	at := s.now()
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" || !text(in.Title, 200, false) || !text(in.Description, 10000, true) || len(in.Options) == 0 || at.IsZero() {
		return contracts.Question{}, contracts.ErrInvalid
	}
	if in.ClosesAt != nil {
		in.ClosesAt = new(in.ClosesAt.UTC().Truncate(time.Microsecond))
		if !in.ClosesAt.After(at) {
			return contracts.Question{}, contracts.ErrInvalid
		}
	}
	options := make([]contracts.OptionInput, len(in.Options))
	for i, option := range in.Options {
		options[i], err = optionInput(option)
		if err != nil {
			return contracts.Question{}, err
		}
	}
	q := contracts.Question{Base: base(tx, at), Title: in.Title, Description: in.Description, ClosesAt: in.ClosesAt,
		AllowLinkJoin: in.AllowLinkJoin, Status: contracts.StatusOpen, CreatorID: actor.PersonID, Revision: 1}
	if err := crud.Create(ctx, tx, &q); err != nil {
		return contracts.Question{}, err
	}
	for _, option := range options {
		if _, err := s.addOption(ctx, tx, q.ID, option, at); err != nil {
			return contracts.Question{}, err
		}
	}
	if _, err := addPerson(ctx, tx, q.ID, actor.PersonID, at); err != nil {
		return contracts.Question{}, err
	}
	return q, nil
}

func (s *Service) addOption(ctx context.Context, tx db.Tx[db.Tenant], question uuid.UUID, in contracts.OptionInput, at time.Time) (contracts.Option, error) {
	o := contracts.Option{Base: base(tx, at), QuestionID: question, Title: in.Title, ImageID: in.ImageID,
		Note: in.Note, Link: in.Link, Position: in.Position, Revision: 1}
	if err := s.files(ctx, tx, o, false); err != nil {
		return contracts.Option{}, err
	}
	if err := crud.Create(ctx, tx, &o); err != nil {
		return contracts.Option{}, err
	}
	return o, nil
}

func (s *Service) files(ctx context.Context, tx db.Tx[db.Tenant], o contracts.Option, remove bool) error {
	ids := []uuid.UUID{}
	if o.ImageID != nil && !remove {
		if _, err := s.images.Resolve(ctx, tx, *o.ImageID, richtext.Workspace); err != nil {
			return err
		}
		ids = append(ids, *o.ImageID)
	}
	if err := s.uses.SetUses(ctx, tx, rest.UsesInput{Module: "decide", Entity: "option", Record: o.ID, Field: "image_id", Files: ids}); err != nil {
		return err
	}
	return s.shown.SetShown(ctx, tx, rest.ShownInput{Module: "decide", Entity: "option", Record: o.ID, Files: ids})
}

func addPerson(ctx context.Context, tx db.Tx[db.Tenant], question, person uuid.UUID, at time.Time) (contracts.Participant, error) {
	p := contracts.Participant{Base: base(tx, at), QuestionID: question, PersonID: person, JoinedAt: at, Revision: 1}
	if err := crud.Create(ctx, tx, &p); err != nil {
		return contracts.Participant{}, err
	}
	return p, nil
}

func (s *Service) Close(ctx context.Context, tx db.Tx[db.Tenant], in contracts.QuestionInput) (contracts.Question, error) {
	snap, actor, err := s.locked(ctx, tx, in.QuestionID, true)
	if err != nil {
		return contracts.Question{}, err
	}
	at := s.now()
	if err := creator(snap, actor, in, at, false); err != nil {
		return contracts.Question{}, err
	}
	return closeQuestion(ctx, tx, snap.Question, new(actor.PersonID), "creator", at)
}

func closeQuestion(ctx context.Context, tx db.Tx[db.Tenant], q contracts.Question, actor *uuid.UUID, reason string, at time.Time) (contracts.Question, error) {
	if q.Status == contracts.StatusClosed {
		return q, nil
	}
	q.Status, q.ClosedAt = contracts.StatusClosed, new(at)
	if err := advance(tx, &q, at); err != nil {
		return contracts.Question{}, err
	}
	if err := events.Succeed(ctx, tx, at); err != nil {
		return contracts.Question{}, err
	}
	if err := events.Publish(ctx, tx, contracts.EventClosed, contracts.Closed{QuestionID: q.ID, QuestionRevision: q.Revision, ClosedBy: actor, Reason: reason, At: at}); err != nil {
		return contracts.Question{}, err
	}
	return q, nil
}

func (s *Service) AddOption(ctx context.Context, tx db.Tx[db.Tenant], in contracts.AddOptionInput) (contracts.OptionChange, error) {
	snap, actor, err := s.locked(ctx, tx, in.QuestionID, true)
	if err != nil {
		return contracts.OptionChange{}, err
	}
	at := s.now()
	if err := creator(snap, actor, in.QuestionInput, at, true); err != nil {
		return contracts.OptionChange{}, err
	}
	option, err := optionInput(in.Option)
	if err != nil {
		return contracts.OptionChange{}, err
	}
	if snap.Question.Revision == math.MaxInt64 {
		return contracts.OptionChange{}, contracts.ErrExhausted
	}
	o, err := s.addOption(ctx, tx, in.QuestionID, option, at)
	if err != nil {
		return contracts.OptionChange{}, err
	}
	if err := advance(tx, &snap.Question, at); err != nil {
		return contracts.OptionChange{}, err
	}
	return contracts.OptionChange{Question: snap.Question, Option: o}, nil
}

func (s *Service) RemoveOption(ctx context.Context, tx db.Tx[db.Tenant], in contracts.RemoveOptionInput) (contracts.Question, error) {
	snap, actor, err := s.locked(ctx, tx, in.QuestionID, true)
	if err != nil {
		return contracts.Question{}, err
	}
	at := s.now()
	if err := creator(snap, actor, in.QuestionInput, at, true); err != nil {
		return contracts.Question{}, err
	}
	if in.OptionID == uuid.Nil {
		return contracts.Question{}, contracts.ErrInvalid
	}
	var target *contracts.Option
	active := 0
	for i := range snap.Options {
		o := &snap.Options[i]
		if o.RemovedAt == nil {
			active++
			if o.ID == in.OptionID {
				target = o
			}
		}
	}
	if target == nil {
		return snap.Question, nil
	}
	if active == 1 {
		return contracts.Question{}, contracts.ErrLastOption
	}
	if snap.Question.Revision == math.MaxInt64 || target.Revision == math.MaxInt64 {
		return contracts.Question{}, contracts.ErrExhausted
	}
	if err := s.files(ctx, tx, *target, true); err != nil {
		return contracts.Question{}, err
	}
	target.RemovedAt, target.UpdatedAt, target.Revision = new(at), at, target.Revision+1
	if err := tx.DB().Model(target).Select("removed_at", "updated_at", "revision").UpdateColumns(target).Error; err != nil {
		return contracts.Question{}, err
	}
	if err := advance(tx, &snap.Question, at); err != nil {
		return contracts.Question{}, err
	}
	return snap.Question, nil
}

func (s *Service) AddParticipant(ctx context.Context, tx db.Tx[db.Tenant], in contracts.AddParticipantInput) (contracts.ParticipantChange, error) {
	if in.PersonID == uuid.Nil {
		return contracts.ParticipantChange{}, contracts.ErrInvalid
	}
	snap, actor, err := s.locked(ctx, tx, in.QuestionID, true, in.PersonID)
	if err != nil {
		return contracts.ParticipantChange{}, err
	}
	at := s.now()
	if err := creator(snap, actor, in.QuestionInput, at, true); err != nil {
		return contracts.ParticipantChange{}, err
	}
	return insertParticipant(ctx, tx, snap, in.PersonID, at)
}

func insertParticipant(ctx context.Context, tx db.Tx[db.Tenant], snap contracts.Snapshot, person uuid.UUID, at time.Time) (contracts.ParticipantChange, error) {
	for _, p := range snap.Participants {
		if p.PersonID == person {
			return contracts.ParticipantChange{Question: snap.Question, Participant: p}, nil
		}
	}
	if snap.Question.Revision == math.MaxInt64 {
		return contracts.ParticipantChange{}, contracts.ErrExhausted
	}
	p, err := addPerson(ctx, tx, snap.Question.ID, person, at)
	if err != nil {
		return contracts.ParticipantChange{}, err
	}
	if err := advance(tx, &snap.Question, at); err != nil {
		return contracts.ParticipantChange{}, err
	}
	return contracts.ParticipantChange{Question: snap.Question, Participant: p}, nil
}

func (s *Service) IssueInvite(ctx context.Context, tx db.Tx[db.Tenant], in contracts.IssueInviteInput) (contracts.InviteReply, error) {
	snap, actor, err := s.locked(ctx, tx, in.QuestionID, true)
	if err != nil {
		return contracts.InviteReply{}, err
	}
	at := s.now()
	if err := creator(snap, actor, in.QuestionInput, at, true); err != nil {
		return contracts.InviteReply{}, err
	}
	q, expiry := snap.Question, in.ExpiresAt.UTC().Truncate(time.Second)
	if !q.AllowLinkJoin {
		return contracts.InviteReply{}, contracts.ErrJoinDisabled
	}
	if expiry.Unix() <= 0 || !expiry.After(at) || (q.ClosesAt != nil && expiry.After(*q.ClosesAt)) {
		return contracts.InviteReply{}, contracts.ErrInvalid
	}
	if q.InviteGeneration == math.MaxInt64 {
		return contracts.InviteReply{}, contracts.ErrExhausted
	}
	q.InviteGeneration, q.InviteExpiresAt = q.InviteGeneration+1, new(expiry)
	if err := advance(tx, &q, at); err != nil {
		return contracts.InviteReply{}, err
	}
	token := s.signer.invite(contracts.InviteClaims{TenantID: q.TenantID, QuestionID: q.ID, Generation: q.InviteGeneration, ExpiresAt: expiry})
	return contracts.InviteReply{QuestionRevision: q.Revision, Token: token, ExpiresAt: expiry}, nil
}

func (s *Service) RevokeInvite(ctx context.Context, tx db.Tx[db.Tenant], in contracts.QuestionInput) (contracts.Question, error) {
	snap, actor, err := s.locked(ctx, tx, in.QuestionID, true)
	if err != nil {
		return contracts.Question{}, err
	}
	at := s.now()
	if err := creator(snap, actor, in, at, false); err != nil {
		return contracts.Question{}, err
	}
	q := snap.Question
	if q.InviteExpiresAt == nil {
		return q, nil
	}
	if q.InviteGeneration == math.MaxInt64 {
		return contracts.Question{}, contracts.ErrExhausted
	}
	q.InviteGeneration, q.InviteExpiresAt = q.InviteGeneration+1, nil
	if err := advance(tx, &q, at); err != nil {
		return contracts.Question{}, err
	}
	return q, nil
}

func (s *Service) Join(ctx context.Context, tx db.Tx[db.Tenant], in contracts.JoinInput) (contracts.ParticipantChange, error) {
	claims, err := s.signer.readInvite(in.Token)
	if err != nil {
		return contracts.ParticipantChange{}, err
	}
	if err := scope(ctx, tx); err != nil {
		return contracts.ParticipantChange{}, err
	}
	if claims.QuestionID != in.QuestionID || claims.TenantID != db.TenantOf(tx).ID {
		return contracts.ParticipantChange{}, contracts.ErrInvite
	}
	snap, actor, err := s.locked(ctx, tx, in.QuestionID, true)
	if err != nil {
		return contracts.ParticipantChange{}, err
	}
	at := s.now()
	if _, err := contracts.AdmitJoin(snap, actor, claims, in.IfRevision, at); err != nil {
		return contracts.ParticipantChange{}, err
	}
	return insertParticipant(ctx, tx, snap, actor.PersonID, at)
}
