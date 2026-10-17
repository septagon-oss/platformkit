# You are writing one round of the platformkit loop, in a cloud VM

This checkout is the task branch of `task-T-0337-the-kernel-counts-what-each-resource-holds` (the head the loop's
last round left), plus one commit that added this `.cloud-round/` directory. Do the round exactly as the loop would
have a local worker do it.

1. Read `.cloud-round/HOUSE.md` (the house rules, normally your system prompt), then `.cloud-round/PROMPT.md` (this
   round's prompt: implement attempt 2, a fix round after a failed gate).
2. Paths in PROMPT.md belong to the loop host. Map them:
   - `/home/jplr/pkit-999/worktrees/task-T-0337-…` (WORKTREE) is this checkout.
   - `/home/jplr/.local/share/pkit-999/state/task-T-0337-…/<file>` (STATE) is `.cloud-round/state/<file>`:
     BRIEF.md, SPECIFY.md, IMPLEMENT.md, TRACE.md, gate-implement.log.
   - `/home/jplr/.local/share/pkit-999/gates/commits_gate.py` is `.cloud-round/tools/commits_gate.py`. Run it as
     `python3 .cloud-round/tools/commits_gate.py . <base>`, where `<base>` is the commit *before* the `.cloud-round`
     commit (`git rev-parse HEAD~0` at the start, minus that commit: use `git log --format=%H -1 -- .cloud-round`
     and take its parent).
   - `/home/jplr/.local/share/pkit-999/mirrors/platformkit-catalog`: `git clone https://github.com/septagon-dev/platformkit-catalog /tmp/catalog`
     if the round needs it.
   - Anything else under `/home/jplr` (the architecture record, `state/shelf`): not reachable. Say so under
     `Not verified:` rather than guessing.
3. Before any test: `~/.pkit/services.sh up`. The stack is on compose.yaml's default ports, and the
   `PLATFORMKIT_TEST_*` variables are already exported. A single command may run at most 10 minutes; run long suites
   per package.
4. Commit your work as the house rules say: Conventional subjects, `Verified:` and `Not verified:` paragraphs. **Never
   modify, move or delete anything under `.cloud-round/`.**
5. Finish with one more commit, `cloud-round: notes`, that adds only `.cloud-round/out/IMPLEMENT.md`: what IMPLEMENT.md
   would hold for this round (the steps, the commands and their output, the result). Its message ends with exactly one
   trailer: `Cloud-Round: complete`, or `Cloud-Round: blocked <the decision needed>`.
6. Push everything to the new branch `cloud/T-0337/implement-2` on origin (`git push origin HEAD:refs/heads/cloud/T-0337/implement-2`).
   Push nothing else, anywhere.
