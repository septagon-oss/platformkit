#!/usr/bin/env python3
"""Commit gate: budget commit first and alone; every feature commit carries Verified:/Not verified:."""
import subprocess, sys
# The base ref is the second argument, defaulting to origin/main so a module's or an app's gate is
# unchanged; a repository with no remote (the loop's own) is counted against its local base (§3.3).
wt, base = sys.argv[1], sys.argv[2] if len(sys.argv) > 2 else 'origin/main'
log = subprocess.run(['git', 'log', '--format=%H%x00%s%x00%b%x01', f'{base}..HEAD'], cwd=wt, capture_output=True, text=True).stdout
commits = [c.split('\x00') for c in log.split('\x01') if c.strip()]
commits = [(h.strip(), s, b) for h, s, b in commits][::-1]

def is_budget_path(path):
    """True for the three shapes a budget ledger takes, and nothing else.

    Written for this file by the repository it gates: `tools/locbudget/README.md`
    specifies it, measured over septagon-clients, where the ledger discovers 35 budget
    files and the old `endswith` test recognised 18 — missing every `e2e/budget/<slug>.json`.
    That gap let a feature commit raise `collect_e2e` (2,454 lines priced at 2,454) unwatched
    and refused a `build(budget)` commit that legitimately moved an owner file, which is what
    blocked task-T-0094 from committing the screens it captures.

    `endswith` was also too loose in the other direction: any path ending in those names
    matched, wherever it sat.
    """
    parts = path.split('/')
    if path in ('loc-budget.json', 'packages-budget.json', 'style-ledger.json'):
        # The programme's own ledgers. `style-ledger.json` since clients PR 154 (2026-10-07): a `build(budget)` commit may
        # carry it, as `tools/locbudget`'s raiseAllowed and `scripts/style_ratchet.py` now both agree.
        return True
    if len(parts) == 3 and parts[2] == 'budget.json' and parts[1] and parts[0] in ('modules', 'clients'):
        return True                                           # modules/<slug>/, clients/<slug>/
    return (len(parts) == 3 and parts[0] == 'e2e' and parts[1] == 'budget'
            and parts[2].endswith('.json') and parts[2] != 'budget.json')   # e2e/budget/<slug>.json


def parent_count(h):
    out = subprocess.run(['git', 'rev-list', '--parents', '-n', '1', h],
                         cwd=wt, capture_output=True, text=True).stdout.split()
    return max(0, len(out) - 1)


def authored(h):
    """The (status, path) rows this commit is the author of, `--root` so a parentless one is read too.

    A **merge** commit authors none: `git diff-tree` on one prints the combined diff, which is the
    conflicted paths and nothing else, and a forge's "Merge pull request" commit has no paragraph any
    person wrote. This gate's own first line says the requirement is of "every *feature* commit", and
    three merge commits on task-T-0026's branch were being refused for a `Verified:` paragraph that
    cannot exist — the gate refusing what its own sentence does not intend to refuse (2026-09-26).
    """
    if parent_count(h) > 1:
        return []
    out = subprocess.run(['git', 'diff-tree', '--no-commit-id', '-r', '--name-status', '--root', h],
                         cwd=wt, capture_output=True, text=True).stdout
    rows = []
    for line in out.splitlines():
        parts = line.split('\t')
        if len(parts) >= 2:
            rows.append((parts[0][0], parts[-1]))
    return rows


fails = []
if not commits:
    fails.append('no commits on the branch')
# A merge joins two histories and writes no diff of its own, so it is not one of the commits these
# rules are about. Filtering here rather than inside the loop keeps the "before the feature commits"
# ordering question asked of authored commits only — a merge landing between two of them says nothing
# about whether a budget commit came first.
authored_commits = [(h, s, b) for h, s, b in commits if parent_count(h) <= 1]
budget_touched = False
for i, (h, s, b) in enumerate(authored_commits):
    rows = authored(h)
    files = [path for _, path in rows]
    touches_budget = any(is_budget_path(f) for f in files)
    # Creating a ledger is not raising a ceiling. The rule exists to stop a feature commit moving a
    # number nobody is watching; a commit that ADDS the file invents the number in the open, in one
    # reviewable diff, and there is no prior value it could have raised. Measured on task-T-0026:
    # `6e7a71f feat(locbudget): read one budget file per owner` adds seventeen `e2e/budget/<slug>.json`
    # and modifies none, and was refused as though it had raised them.
    modifies_budget = any(is_budget_path(path) for st, path in rows if st != 'A')
    if s.startswith('build(budget)'):
        if any(not is_budget_path(f) for f in files):
            fails.append(f'{h[:7]} build(budget) touches non-budget files')
        if budget_touched is False and i != 0 and not all(c[1].startswith('build(budget)') for c in authored_commits[:i]):
            fails.append(f'{h[:7]} build(budget) is not before the feature commits')
        budget_touched = True
    else:
        if modifies_budget:
            fails.append(f'{h[:7]} feature commit edits a budget file')
        # Root's record commits (`chore(record): root …`, decision 0008: a reviewer-pin edit or adoption only root may
        # write) carry their authorisation in the subject; the verification they answer for is the next round's
        # deciding commands, not theirs. Measured 2026-10-12: three task branches refused at root's own commit.
        root_record = s.startswith('chore(record): root')
        if 'Verified:' not in b and not root_record:
            fails.append(f'{h[:7]} has no Verified: paragraph')
        if 'Not verified' not in b and not root_record:
            fails.append(f'{h[:7]} has no Not verified: line')
if fails:
    print('COMMIT GATE FAILED'); print('\n'.join(' - ' + f for f in fails)); sys.exit(1)
print('commit gate: ok (%d commits, %d authored)' % (len(commits), len(authored_commits)))
