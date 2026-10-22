package main

import (
	"fmt"
	"strings"
)

func (h *history) transition(sha string) ([]string, error) {
	out, err := h.repo.git(nil, "rev-list", "--parents", "-n", "1", sha)
	if err != nil {
		return nil, err
	}
	parents := strings.Fields(string(out))[1:]
	raw, err := h.repo.git(nil, "cat-file", "commit", sha)
	if err != nil {
		return nil, err
	}
	header, _, _ := strings.Cut(string(raw), "\n\n")
	if strings.Count("\n"+header, "\nparent ") != len(parents) {
		return nil, fmt.Errorf("%s: shallow history hides required parents", sha)
	}
	child, err := h.budgetAt(sha)
	if err != nil {
		return nil, err
	}
	var states []budgetState
	for _, parent := range parents {
		b, err := h.budgetAt(parent)
		if err != nil {
			return nil, err
		}
		states = append(states, b)
		if !child.present && b.present {
			return []string{fmt.Sprintf("%s: deleted established %s from parent %s", sha, budgetFile, parent)}, nil
		}
	}
	if !child.present {
		return nil, nil
	}
	if len(parents) > 1 {
		base, err := uniqueBase(h.repo, parents)
		if err != nil {
			return nil, err
		}
		common, err := h.budgetAt(base)
		if err != nil {
			return nil, err
		}
		values := make([]budget, len(states))
		for i, b := range states {
			values[i] = h.values(b)
		}
		inherited := inheritedBudget(h.values(common), values)
		return rises("merge "+sha, inherited, child.values, "exceeds inherited ceiling; a merge cannot authorize a raise"), nil
	}
	parent := budgetState{}
	if len(states) == 1 {
		parent = states[0]
	}
	if !parent.present && !h.initial.present {
		return rises(sha+" initialization against "+h.base, h.initial.values, child.values, "initial ceiling exceeds measured baseline"), nil
	}
	if len(rises(sha, h.values(parent), child.values, "")) == 0 {
		return nil, nil
	}
	reason, err := h.approval(sha, parents)
	if err != nil {
		return nil, err
	}
	if reason == "" {
		return nil, nil
	}
	return rises(sha, h.values(parent), child.values, reason), nil
}

func inheritedBudget(common budget, parents []budget) budget {
	result := zeroBudget()
	for _, key := range measures {
		value, changed := common[key], false
		for _, parent := range parents {
			if parent[key] != common[key] {
				if !changed || parent[key] < value {
					value = parent[key]
				}
				changed = true
			}
		}
		result[key] = value
	}
	return result
}

func (h *history) approval(sha string, parents []string) (string, error) {
	out, err := h.repo.git(nil, "show", "-s", "--format=%s%n%(trailers:key=Owner-approved,valueonly)", sha)
	if err != nil {
		return "", err
	}
	subject, trailer, _ := strings.Cut(string(out), "\n")
	var reasons []string
	if !strings.HasPrefix(subject, "build(complexity):") {
		reasons = append(reasons, "raise requires subject build(complexity):")
	}
	if strings.TrimSpace(trailer) == "" {
		reasons = append(reasons, "raise requires nonempty Owner-approved: trailer")
	}
	args := []string{"diff-tree", "--no-commit-id", "--name-only", "--no-renames", "-r", "-z", "--root"}
	if len(parents) != 0 {
		args = append(args, parents[0])
	}
	out, err = h.repo.git(nil, append(args, sha)...)
	if err != nil {
		return "", err
	}
	if string(out) != budgetFile+"\x00" {
		reasons = append(reasons, "raise must change only "+budgetFile)
	}
	return strings.Join(reasons, "; "), nil
}
