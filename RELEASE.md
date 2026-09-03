# Releasing

`v1.0.0` is the first tag, and the procedure below is the same one every tag
after it follows. It has four parts: what has to be true before the tag, the
tag, what the tag does on its own, and what the two private repositories do
afterwards. The first push onto the public repository happens once, and only the owner can do it.

Everything here is a command somebody runs, in the order they run it. Nothing
in it is automatic except the part that says so.

## 1. Preconditions

None of these are advisory. A tag that skips one is a tag whose image nobody
should pull.

```sh
git status --short          # empty: a release is of what is committed
make check                  # gates 1-9
make e2e                    # gate 10, which needs node and a browser
govulncheck ./...           # if it is installed; see below
go run ./tools/locbudget --check   # every bucket under its ceiling
```

`go vet` is inside `make check` and is not run again. `make check` is what CI
runs on the tag as well — the release workflow's first job is that same command
against the tagged tree — so running it here is about not finding out in a
workflow what a laptop could have said in four minutes.

**govulncheck** is not a gate, because a gate that depends on a database of
somebody else's making can fail on a morning when nothing in this repository
changed. It is a precondition instead: `go install
golang.org/x/vuln/cmd/govulncheck@latest` and run it, and every finding is
either fixed before the tag or written into the release notes with the reason it
is not reachable. If it cannot be installed, say so in the notes rather than
leaving the reader to assume it was clean.

**License headers.** There are none, and that is the state to preserve: `LICENSE`
at the root is Apache-2.0 and covers every file, `NOTICE` names what was pulled
from elsewhere and under what licence. Before a tag:

```sh
# No .go file may carry a copyright or licence header of its own. A file that
# does came from somewhere this repository has not accounted for.
grep -rlnE 'Copyright|SPDX-License-Identifier' --include='*.go' kit ui design modules apps tools
```

Empty output is the passing result. If it is not empty, the file's provenance
goes into `NOTICE` before the tag, or the file goes.

**Budgets are a published commitment** (ADR 0009), so the tag is where they are
made honest. The owner runs the ratchet once, reads the diff, and commits it on
its own:

```sh
go run ./tools/locbudget --write
git diff loc-budget.json          # every number went down or stayed
git commit -m "chore: re-baseline the ceilings for v1.0.0" loc-budget.json
```

The ratchet only ever lowers a ceiling. If a number went up, something edited
the file by hand and CI would have caught it; find out what before tagging.

## 2. The tag

```sh
git tag -a v1.0.0 -m "PlatformKit 1.0.0"
git push origin main --follow-tags
```

**Signing is the owner's choice, and both answers are defensible.** A signed tag
is `git tag -s` instead of `-a`, and it needs a GPG or SSH signing key already
configured (`git config user.signingkey`, `git config gpg.format ssh` for an SSH
key). GitHub then shows the tag as Verified, and `git verify-tag v1.0.0` is a
check anybody can run.

- **Sign it** if the project will ever be consumed by somebody who has a reason
  to care who tagged it — which is every project that publishes a container
  image. This is the recommendation.
- **Do not sign it** if there is no key the owner is willing to keep, because an
  unsigned tag is honest and a signing key stored badly is worse than none. An
  annotated tag still carries the tagger, the date and the message.

The choice is per tag, not per project: starting unsigned and signing from
`v1.1.0` costs nothing and confuses nobody.

Do not move a tag. If `v1.0.0` was wrong, `v1.0.1` is the fix; the release
workflow writes the image digest into the notes precisely so that a moved tag
would be visible.

## 3. What the tag triggers

`.github/workflows/release.yml` runs on any `v*` tag and does four things,
in one workflow, with `contents: read` at the top and rights granted per job:

1. **`make check` and `make e2e` against the tagged tree**, with Postgres,
   NATS and a browser, the same way `ci.yml` does — and only after the job has
   proved the tag is an ancestor of `main`, because a tag is the one trigger
   here and anything anybody could tag was otherwise a release. Everything below
   is `needs: check`.
2. **Builds `deploy/Dockerfile` and pushes to GHCR** as
   `ghcr.io/septagon-oss/platformkit:<tag>`. `latest` moves only when the tag
   has no pre-release part, so `v1.1.0-rc.1` publishes under its own name and
   moves nothing.
3. **Generates an SBOM** of the image — not of the checkout, because what a
   reader wants is what they are about to run — with `syft` via
   `anchore/sbom-action`, as `platformkit-<tag>.spdx.json`.
4. **Creates the GitHub release** with `--generate-notes`, the SBOM attached,
   and the image digest as the first line of the body.

Every action in both workflows is pinned by commit SHA with the version in a
trailing comment; Dependabot rewrites the two together, weekly, which is the
only way a pin stays current. `ci.yml` was not pinned when this paragraph first
claimed it was, which the release review found: a claim about a supply chain is
worth checking before it is worth writing.

The base images in `deploy/Dockerfile` are pinned by digest for the same reason,
and `.dockerignore` keeps the git history, any local `config.yaml` and anything
uploaded out of the build context.

Nothing in the workflow needs a secret beyond `GITHUB_TOKEN`: GHCR accepts it
for the repository's own package.

## 4. The first push onto the existing public repository `[OWNER ONLY]`

`github.com/septagon-oss/platformkit` already exists and is public: it holds the
0.x CLI scaffolder (releases to v0.15.1), with its own stars and forks. The
extracted repository has an unrelated history, so the first push is the publish,
and it is done without a force push so that every 0.x commit and tag stays
reachable and every fork diverges cleanly:

```sh
git remote add origin git@github.com:septagon-oss/platformkit.git && git fetch origin
git branch legacy-0.x origin/main && git push origin legacy-0.x
git merge --allow-unrelated-histories -s ours origin/main \
  -m "v1: the extracted reference architecture replaces the 0.x scaffolder; its history stays under legacy-0.x"
make check && git push origin main
```

`-s ours` keeps this tree byte for byte and makes the 0.x line an ancestor of
`main`, which is what the release workflow's ancestry check wants. Then the tag
(section 2) and, once, the settings below. Every step is in the GitHub web UI or
`gh`, and none of them is in this repository.

- [ ] **Settings → Branches → Add rule for `main`:** require a pull request,
      require status checks to pass, and require branches to be up to date.
- [ ] **Settings → Rules → Rulesets → New tag ruleset**, targeting `v*`:
      restrict creation and deletion to the owner, and block force pushes. The
      release workflow already refuses a tag that is not an ancestor of `main`,
      so this is the second half — the workflow says which commits may be
      released, and the ruleset says who may say so. A tag is not a branch and
      branch protection does not cover one.
- [ ] **Required status checks: `check`** — the job in `ci.yml`, by that name.
      It is the only one; ten gates behind one name is the point.
- [ ] **Settings → Code security → Secret scanning: on**, and **push protection:
      on**. A public repository gets both for free and there is no reason to
      decline.
- [ ] **Settings → Code security → Dependabot:** alerts on and security updates
      on. The weekly version bumps are already declared in
      `.github/dependabot.yml` (gomod, npm in `e2e/`, github-actions), so this
      switch is only about the security half.
- [ ] **Settings → Actions → General → Workflow permissions: read-only**, with
      "Allow GitHub Actions to create and approve pull requests" off. Both
      workflows ask for what they need per job.
- [ ] **Packages → platformkit → Package settings → visibility: public**, so the
      image is pullable without a token. The package inherits nothing from the
      repository; this is a separate switch and it is easy to miss.
- [ ] **Run the five commands from a machine with no credentials** against the
      public clone URL. `scripts/start.sh` is the one-liner; it is what the
      README promises and the first push is where that promise becomes checkable.

## 5. Consumers

Consumers pin the tag: each of them replaces whatever it resolved the public
module from with `go get github.com/septagon-oss/platformkit@v1.0.0`, and how
that is done is written down in the consumer, not here.

## Checklist

Ten lines, in order.

1. `git status --short` is empty.
2. `make check` is green.
3. `make e2e` is green.
4. `govulncheck ./...` is clean, or every finding is in the notes with a
   reason. CI runs it on every pull request, so this is a confirmation.
5. No `.go` file carries a copyright or licence header; `NOTICE` accounts for
   everything pulled.
6. `go run ./tools/locbudget --write --round 100` and `scripts/check_packages.sh
   --write`, committed on their own. A ceiling is the count rounded up to the
   next hundred: at exactly the count, the next one-line pull request fails.
7. `git tag -a v1.0.0 -m "PlatformKit 1.0.0"` — or `-s`, the owner's choice.
8. `git push origin main --follow-tags`, and watch `release.yml` go green.
9. The settings checklist in section 4, once.
10. Consumers pin the tag.
