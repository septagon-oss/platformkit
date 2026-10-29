#!/usr/bin/env python3
"""Refuse a 15-line comment block in a production file this branch changed under the kernel packages the comment rule was paid down in.

`kit/httpx`, `kit/events`, `kit/db`, `kit/rest` and `kit/app` hold the host gate, the outbox, the
tenancy transaction, the resource port and the composition — the code every module reads before it
reads a docstring. A block there repeats what the lines around it already say, and the next person
opens all of it to change one line, so the rule is short: a comment states why, in a few lines, and
the story of how it came to be is `git log`'s.

Two things make this a case about one branch rather than a census of the tree:

* **Which files it reads.** The `.go` files between `BASE_REF` (default `origin/main`) and `HEAD`
  under those five packages, excluding tests and `testdata/`. The delivery is the sole writer of the
  comments in the files it rewrote, so growth in an unrelated file — main's own prose, or another
  branch's — is not this branch's to answer for, and refusing it would tax every branch that has not
  merged yet. The whole-tree shape is priced elsewhere and already: `tools/complexity` counts
  `long_comment_blocks` across the checkout and refuses the branch whose total exceeds what it priced,
  and a total is blind per package — a branch may shorten a block in `modules/` while growing one here
  and report the same number. This reads the changed files individually.
* **What it asks.** One threshold, from the rule itself, with no figure to re-measure when the tree
  moves: a block of `CEILING + 1` lines or more is refused, by name and by the line it starts on.

Only `//` runs are counted. A `/* */` span cannot be recognised from text without a parser — a doc
comment containing `ui/*` would open one — and this repository comments in `//`.

Test form: run it, with no argument. It reads `git log` and the working tree only, starts nothing, and
answers in a few hundred milliseconds: exit 0 names the longest block it saw and how many files it
read, exit 1 lists each block over the threshold, exit 2 when the base commit cannot be resolved (a
shallow checkout, or a `BASE_REF` that is not a commit) because then it would read no files and pass.
"""

import os
import subprocess
import sys

AREAS = ("kit/httpx/", "kit/events/", "kit/db/", "kit/rest/", "kit/app/")
CEILING = 14  # a block of 15 or more is the offender this rule shortens or moves to a README


def run(root, *argv):
    """One git command, taken at the checkout's root wherever the caller stood.

    Pathspecs are CWD-relative, so asked from `kit/httpx` the area `kit/httpx/` would match nothing,
    the walk would read no file and the answer would be a silent pass. Every read here goes through
    this, so the answer is the repository's and not the directory's.
    """
    return subprocess.run(["git", "-C", root, *argv], capture_output=True, text=True)


def changed_go_files(root, base):
    """The non-test `.go` files under AREAS that this branch rewrote since `base`.

    A branch with no such file is a branch this rule has nothing to say about; the empty list is a
    passing answer, not a missing one.
    """
    diff = run(root, "diff", "--name-only", f"{base}...HEAD", "--", *AREAS)
    if diff.returncode != 0:
        return []
    return [f for f in diff.stdout.split()
            if f.endswith(".go") and not f.endswith("_test.go") and "/testdata/" not in f]


def block_lengths(path):
    """The length of every `//` block in `path`, with the line each one starts on."""
    blocks, run_length, start = [], 0, 0
    with open(path, encoding="utf-8") as source:
        for number, line in enumerate(source, 1):
            if line.lstrip().startswith("//"):
                if run_length == 0:
                    start = number
                run_length += 1
            elif run_length:
                blocks.append((run_length, start))
                run_length = 0
    if run_length:
        blocks.append((run_length, start))
    return blocks


def main(_argv):
    probe = subprocess.run(["git", "rev-parse", "--show-toplevel"], capture_output=True, text=True)
    if probe.returncode != 0:
        print("comment blocks: no checkout to read here (git rev-parse refused), so nothing was checked",
              file=sys.stderr)
        return 2
    root = probe.stdout.strip()
    base_ref = os.environ.get("BASE_REF", "origin/main")
    base = run(root, "merge-base", base_ref, "HEAD")
    if base.returncode != 0:
        print(f"comment blocks: {base_ref} resolves to no merge base with HEAD, so there is no diff to "
              f"read — name a real BASE_REF rather than pass on no files", file=sys.stderr)
        return 2
    files = changed_go_files(root, base.stdout.strip())
    offenders, longest = [], 0
    for name in files:
        for length, start in block_lengths(os.path.join(root, name)):
            longest = max(longest, length)
            if length > CEILING:
                offenders.append(f"  {name}:{start}  {length}-line comment block")
    if offenders:
        print(f"comment blocks over {CEILING} lines, in the files this branch changed under "
              f"{', '.join(AREAS)}:", file=sys.stderr)
        print("\n".join(sorted(offenders)), file=sys.stderr)
        print("A comment states why, in a few lines. What the code already says is deleted; the story of "
              "how it came to be is git log's; prose that still matters moves to the package's README or "
              "docs/, linked in one line.", file=sys.stderr)
        return 1
    print(f"comment blocks: longest is {longest} lines across the {len(files)} production files this "
          f"branch rewrote under {', '.join(AREAS)}; a block of {CEILING + 1} or more is refused")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
