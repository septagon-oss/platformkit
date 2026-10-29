#!/usr/bin/env python3
"""The digest a landing commit records is the case the tree still holds.

A case is evidence about served behaviour only while nobody refused by it can answer the refusal by
rewriting the case. The general rule, and a table of three digests, live beside this file in
`a_refused_case_keeps_the_bytes_it_was_landed_with_test.py`. The guard cases this branch landed in
`kit/rest` sit outside that table, and each refuses one clause of the mount-site rule that lets a
generated query be reached only under one workspace read guard. A clause can stop deciding — another
clause can shadow it, so the guard reads stricter than it is and the whole package still answers ok —
and the only footing for "this case still asks what it was landed asking" is the bytes it arrived in.

What is asked here is therefore not a second table of digests that can drift from the first: it is the
digest in the body of the commit that added the file, read from the claim above that commit's
`Verified:` paragraph, measured against the tree. Whoever means to replace one of these cases has to
pass through the commit that recorded its bytes, which is where a reader can see what every assertion
still asks. A test file whose newest commit records no digest is left alone: nothing claims its bytes,
so nothing is contradicted here. On main, and on a checkout that cannot find its merge base, the branch
holds no landing of its own and the walk asks nothing.

Run: python3 -m pytest -q \
    scripts/reviews/the_digest_a_landing_commit_records_is_the_case_in_the_tree_test.py
"""
import hashlib
import os
import re
import subprocess

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

# The digest a landing commit claims for the file it adds.
DIGEST = re.compile(r"\b([0-9a-f]{64})\b")
TEST_PATHS = ("_test.go", "_test.py")


def git(*args):
    return subprocess.run(("git", *args), cwd=ROOT, capture_output=True, text=True, check=False).stdout


def merge_base():
    """Where this branch parts from origin/main, or None when no such parting can be read — a
    checkout that never fetched origin/main, and main itself, where HEAD is the merge base."""
    base = git("merge-base", "origin/main", "HEAD").strip()
    if not base or base == git("rev-parse", "HEAD").strip():
        return None
    return base


def landed_cases(base):
    """Every test file between the merge base and HEAD whose newest touching commit records one
    digest for one test file in its claim: the file, that commit, and the digest it claims.

    A commit is asked only where it used the landing convention this branch's guard cases arrived
    under — it adds one test file, no later commit of this branch has touched that file, and its
    claim says "byte for byte" and records one digest beside it — so the digest cannot be read as the
    bytes of some other file. A claim that pairs a digest with a sibling file would otherwise refuse
    an unrelated branch whose merge base reaches further back, and history holds such claims. A file a later commit rewrote is not asked here: rewriting one is a
    change a reader judges on its own diff, and this case refuses only a file that still stands where
    it landed while its bytes no longer answer the commit that landed it."""
    cases = {}
    for sha in git("rev-list", "--no-merges", f"{base}..HEAD").split():
        added = [p for p in git("show", "--no-commit-id", "--name-only", "--diff-filter=A", "-r", sha).split()
                 if p.endswith(TEST_PATHS)]
        if len(added) != 1:
            continue
        path = added[0]
        newest = git("log", "--format=%H", f"{base}..HEAD", "--", path).split()
        if not newest or newest[0] != sha:
            continue
        claim = git("log", "-1", "--format=%b", sha).split("Verified:")[0]
        if "byte for byte" not in claim.lower():
            continue
        claimed = DIGEST.findall(claim)
        if len(claimed) == 1:
            cases[path] = (sha, claimed[0], claim)
    return cases


def test_every_landed_case_is_the_bytes_its_landing_commit_recorded():
    base = merge_base()
    if base is None:
        return
    for path, (commit, wanted, _) in sorted(landed_cases(base).items()):
        found = None
        try:
            with open(os.path.join(ROOT, path), "rb") as handle:
                found = hashlib.sha256(handle.read()).hexdigest()
        except OSError:
            pass
        assert found == wanted, (
            f"{path}: {commit[:12]} records {wanted} and the tree holds {found}. The bytes a refusal "
            "was written in are the refusal: replace this case in the open, in a change that says "
            "what every assertion still asks")


def test_a_landed_case_is_not_replaced_by_a_file_that_judges_nothing():
    # The digest above catches a changed byte, including one re-recorded in the same change that
    # gutted the case. This catches the same softening in a file whose newest commit re-recorded its
    # digest without saying so: a Go test file still declares Test functions and still refuses
    # through t.Fatal or t.Error. Files whose newest commit claims no digest are not asked — the
    # tree's own conventions for a harness, a fixture or a pin script are theirs, not this case's.
    base = merge_base()
    if base is None:
        return
    for path, (commit, _, _) in sorted(landed_cases(base).items()):
        if not path.endswith(".go"):
            continue
        with open(os.path.join(ROOT, path), encoding="utf-8") as handle:
            body = handle.read()
        declared = body.count("\nfunc Test")
        refused = body.count("t.Fatal") + body.count("t.Error")
        assert declared and refused, (
            f"{path}: {commit[:12]} landed it as a case; the file declares {declared} test function(s) "
            f"and {refused} refusal(s), so it judges nothing")
