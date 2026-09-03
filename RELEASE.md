# Releasing

`v1.0.0` is the first tag, and the procedure below is the same one every tag
after it follows. It has four parts: what has to be true before the tag, the
tag, what the tag does on its own, and what the two private repositories do
afterwards. The visibility flip happens once, and only the owner can do it.

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

1. **`make check` against the tagged tree**, with Postgres and NATS, the same
   way `ci.yml` does. Everything below is `needs: check`.
2. **Builds `deploy/Dockerfile` and pushes to GHCR** as
   `ghcr.io/septagon-oss/platformkit:<tag>`. `latest` moves only when the tag
   has no pre-release part, so `v1.1.0-rc.1` publishes under its own name and
   moves nothing.
3. **Generates an SBOM** of the image — not of the checkout, because what a
   reader wants is what they are about to run — with `syft` via
   `anchore/sbom-action`, as `platformkit-<tag>.spdx.json`.
4. **Creates the GitHub release** with `--generate-notes`, the SBOM attached,
   and the image digest as the first line of the body.

Actions are pinned by commit SHA with the version in a trailing comment. A
version bump is a commit that changes both.

Nothing in the workflow needs a secret beyond `GITHUB_TOKEN`: GHCR accepts it
for the repository's own package.

## 4. The visibility flip `[OWNER ONLY]`

Once, before or immediately after `v1.0.0`. Every step is in the GitHub web UI
or `gh`, and none of them is in this repository.

- [ ] **Settings → General → Danger Zone → Change visibility → Public.**
      `gh repo edit septagon-oss/platformkit --visibility public --accept-visibility-change-consequences`
- [ ] **Settings → Branches → Add rule for `main`:** require a pull request,
      require status checks to pass, and require branches to be up to date.
- [ ] **Required status checks: `check`** — the job in `ci.yml`, by that name.
      It is the only one; ten gates behind one name is the point.
- [ ] **Settings → Code security → Secret scanning: on**, and **push protection:
      on**. A public repository gets both for free and there is no reason to
      decline.
- [ ] **Settings → Code security → Dependabot:** alerts on, security updates on,
      and a weekly `gomod` + `github-actions` schedule if the owner wants
      version bumps as well as security ones.
- [ ] **Settings → Actions → General → Workflow permissions: read-only**, with
      "Allow GitHub Actions to create and approve pull requests" off. Both
      workflows ask for what they need per job.
- [ ] **Packages → platformkit → Package settings → visibility: public**, so the
      image is pullable without a token. The package inherits nothing from the
      repository; this is a separate switch and it is easy to miss.
- [ ] **Run the five commands from a machine with no credentials** against the
      public clone URL. `scripts/start.sh` is the one-liner; it is what the
      README promises and the flip is where that promise becomes checkable.

## 5. The two private repositories

`platformkit-catalog` and `septagon-clients` resolve the public module from a
sibling checkout today. The tag is what lets them stop. Run these after the tag
is pushed and the module is fetchable — `GOPRIVATE` is not needed once the
repository is public, and `go get` needs the proxy to have seen the tag, which
can take a minute.

`platformkit-catalog` first, because `septagon-clients` depends on it:

```sh
cd ~/gitrepos/platformkit-catalog
sed -i '/^replace github.com\/septagon-oss\/platformkit /d' go.mod
# and the three comment lines above it, by hand: a directive that is gone
# should not leave a paragraph explaining itself.
go get github.com/septagon-oss/platformkit@v1.0.0
go mod tidy
make check
git commit -am "chore: depend on platformkit v1.0.0"
```

Then `septagon-clients`, which drops two directives — the public one, and the
catalog one once the catalog itself is tagged:

```sh
cd ~/gitrepos/septagon-clients
sed -i '/^replace github.com\/septagon-oss\/platformkit /d' go.mod
go get github.com/septagon-oss/platformkit@v1.0.0
# The catalog is private and untagged until its owner tags it. Until then its
# replace directive stays and this is the only line that differs from above.
go mod tidy
make check
git commit -am "chore: depend on platformkit v1.0.0"
```

Both repositories are private, so their CI resolves the public module the
ordinary way and needs no `GOPRIVATE` entry for it. The catalog and the client
repository stay private and therefore keep needing one for each other.

## Checklist

Ten lines, in order.

1. `git status --short` is empty.
2. `make check` is green.
3. `make e2e` is green.
4. `govulncheck ./...` is clean, or every finding is in the notes with a reason.
5. No `.go` file carries a copyright or licence header; `NOTICE` accounts for
   everything pulled.
6. `go run ./tools/locbudget --write` and commit the ratchet on its own.
7. `git tag -a v1.0.0 -m "PlatformKit 1.0.0"` — or `-s`, the owner's choice.
8. `git push origin main --follow-tags`, and watch `release.yml` go green.
9. The flip checklist in section 4, once.
10. The two private repositories drop their `replace` lines and pin the tag.
