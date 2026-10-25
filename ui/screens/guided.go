package screens

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/resource"
)

type guidedPairInput struct {
	ID    uuid.UUID `path:"id"`
	Step  uuid.UUID `path:"step"`
	Set   uuid.UUID `path:"set"`
	Extra int       `query:"extra" minimum:"0" maximum:"86400"`
}

func mountGuided(router *httpx.Router, shell page.Shell, o Options, res httpx.Resource) {
	prefix := "screen-" + res.Module + "-" + res.Entity + "-session-"
	for _, mode := range []string{"resume", "finish", "summary"} {
		path := res.Path + "/{id}"
		if mode != "resume" {
			path += "/" + mode
		}
		page.Serve(router, shell, page.Route{ID: prefix + mode, Method: http.MethodGet, Path: path, Summary: "Guided session"}, res.ReadAuth(),
			func(ctx context.Context, req page.Request, in *itemInput) (page.View, error) {
				p, err := guidedPage(ctx, res, o, req, in.ID, mode)
				if err != nil {
					return page.View{}, err
				}
				if mode == "resume" {
					return page.View{}, httpx.SeeOther(guidedResume(p))
				}
				if p.Session.Progress != nil && p.Session.Progress.Finished != nil && mode != "summary" {
					return page.View{}, httpx.SeeOther(p.Base + "/summary")
				}
				if mode == "summary" && (p.Session.Progress == nil || p.Session.Progress.Finished == nil) {
					return page.View{}, httpx.SeeOther(p.Base)
				}
				return resource.Guided(p, localized(o, req)), nil
			})
	}
	for _, mode := range []string{"step", "rest"} {
		path := res.Path + "/{id}/steps/{step}/sets/{set}"
		if mode == "rest" {
			path = res.Path + "/{id}/rest/{step}/{set}"
		}
		page.Serve(router, shell, page.Route{ID: prefix + mode, Method: http.MethodGet, Path: path, Summary: "Guided session " + mode}, res.ReadAuth(),
			func(ctx context.Context, req page.Request, in *guidedPairInput) (page.View, error) {
				p, err := guidedPage(ctx, res, o, req, in.ID, mode)
				if err != nil {
					return page.View{}, err
				}
				if p.Session.Progress != nil && p.Session.Progress.Finished != nil {
					return page.View{}, httpx.SeeOther(p.Base + "/summary")
				}
				if !guidedFind(&p, in.Step, in.Set) {
					return page.View{}, problem.NotFound("session pair is unavailable")
				}
				if mode == "rest" {
					seconds, owned := p.Rest()
					if !owned {
						return page.View{}, httpx.SeeOther(p.Base)
					}
					p.Extra = min(in.Extra, 86400-int(seconds))
				}
				return resource.Guided(p, localized(o, req)), nil
			})
	}
	mountGuidedCommands(router, shell, res, prefix)
}

func guidedPage(ctx context.Context, res httpx.Resource, o Options, req page.Request, id uuid.UUID, mode string) (resource.GuidedPage, error) {
	s, err := res.Guided(ctx, id)
	if err != nil {
		return resource.GuidedPage{}, rest.Fault(err)
	}
	tx, ok := httpx.TxFrom(ctx)
	if !ok {
		return resource.GuidedPage{}, problem.New(503, "session needs a request transaction")
	}
	actor, _ := tenancy.PrincipalFrom(ctx)
	res.Commands = res.CommandsFor(ctx)
	text := words(o, req)
	policy := resolve(Localise(res, text), text, "")
	exit := o.Workspace
	if !httpx.LocalPath(exit) {
		exit = "/"
	}
	return resource.GuidedPage{Session: s, Policy: guidedPolicy(policy.Session), Base: res.Screen + "/" + id.String(), Exit: exit, Mode: mode,
		Scope: db.TenantOf(tx).ID.String() + "/" + actor.UserID.String() + "/" + res.Module + "/" + res.Entity}, nil
}

// guidedPolicy hands the resolved session presentation to the renderer as the
// plain view it renders: ui/resource reads no policy package.
func guidedPolicy(r *present.ResolvedSession) resource.GuidedPolicy {
	if r == nil {
		return resource.GuidedPolicy{}
	}
	view := func(c present.SessionCapability) resource.GuidedCapability {
		return resource.GuidedCapability{Declared: c.Declared, Callable: c.Command != nil}
	}
	return resource.GuidedPolicy{Complete: view(r.Complete), Finish: view(r.Finish), Labels: r.Labels}
}

func guidedResume(p resource.GuidedPage) string {
	count := 0
	if progress := p.Session.Progress; progress != nil {
		if progress.Finished != nil {
			return p.Base + "/summary"
		}
		count = len(progress.Completed)
	}
	step, set, ok := p.Session.Pair(count)
	if !ok {
		return p.Base + "/finish"
	}
	return p.Base + "/steps/" + p.Session.Steps[step].ID.String() + "/sets/" + p.Session.Steps[step].Sets[set].String()
}

func guidedFind(p *resource.GuidedPage, step, set uuid.UUID) bool {
	for i, s := range p.Session.Steps {
		for j, id := range s.Sets {
			if s.ID == step && id == set {
				p.Step, p.Set = i, j
				return true
			}
		}
	}
	return false
}

func mountGuidedCommands(router *httpx.Router, shell page.Shell, res httpx.Resource, prefix string) {
	for _, command := range res.Commands {
		page.Serve(router, shell, page.Route{ID: prefix + command.Verb, Method: http.MethodPost, Path: res.Path + "/{id}/" + command.Verb, Summary: command.Summary}, command.Auth,
			func(ctx context.Context, _ page.Request, in *itemFormInput) (page.View, error) {
				values, err := rest.GuidedValues(in.RawBody, command.Fields)
				if err != nil {
					return page.View{}, err
				}
				reply, err := command.Execute(ctx, in.ID, values)
				if err != nil {
					return page.View{}, rest.Fault(err)
				}
				target := strings.TrimSuffix(res.Screen, "/") + "/" + in.ID.String()
				if receipt, ok := reply.Result.(entity.GuidedCompletion); ok {
					target += "/rest/" + receipt.StepID.String() + "/" + receipt.SetID.String()
				} else {
					target += "/summary"
				}
				return page.View{}, httpx.SeeOther(target)
			})
	}
}
