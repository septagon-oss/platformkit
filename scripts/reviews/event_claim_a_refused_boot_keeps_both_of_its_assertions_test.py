#!/usr/bin/env python3
"""The committed case for a refused boot still asks both halves of the promise.

kit/app claims an app's declared event names for as long as one of its boots stands, and
gives the claim back when a boot ends — whether the boot ended because the caller Closed
the Runtime or because Start itself refused after the deployment was open. The second is
answered in kit/app/start.go's transport refusal and the first in Runtime.Close; the boot
that follows either one has to take the claim again in prepareRuntime. Nothing but
kit/app/a_boot_refused_at_the_transport_takes_its_names_again_test.go distinguishes those
two answers from each other in the committed suite, and each of them is one call that a
later edit can delete while the case stays green: with the reacquire in prepareRuntime
removed, the case fails at its third step alone, and with the release in Runtime.Close
removed it fails at its second. A case that lost either step would keep passing and would
stop telling anyone what a re-booted composition owes.

So this reads the committed file and asks only what makes it bite, never how it is worded:
the transport constructor refuses exactly one boot; the claim is asked of the process record
at three points in this order — held after New, free after the refusal, held after the retry
— each ask failing on the answer its step forbids; a fourth ask, on the shape the module
really declares, is what keeps the third answer about the payload rather than about the
second call; the same app and the same disagreeing declaration are asked at the three; and no
skip stands between the reader and any of it.

Run: python3 -m pytest -q scripts/reviews/event_claim_a_refused_boot_keeps_both_of_its_assertions_test.py
"""
import json
import os
import re

import pytest

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
CASE = os.path.join(
    ROOT, "kit", "app", "a_boot_refused_at_the_transport_takes_its_names_again_test.go"
)
CASE_ID = CASE.removeprefix(ROOT + os.sep) + "#TestABootRefusedAtTheTransportTakesItsEventNamesAgain"

# One ask of the process-wide record, with the condition under which the case calls it a
# failure: `err == nil` fails when the names are free, `err != nil` when they are claimed.
ASK = re.compile(
    r"if\s+err\s*:=\s*events\.CheckAppDeclared\(\s*(?P<app>[\w.]+)\s*,\s*(?P<decl>\w+)\s*\)\s*;"
    r"\s*err\s*(?P<when>==|!=)\s*nil\s*\{",
)
STARTS = re.compile(r"(?P<var>\w+),\s*err\s*:=\s*a\.Start\(")
FATALS = re.compile(r"t\.(Fatal|Fatalf|Error|Errorf)\(")


def body():
    """The one test function's body, with comments and blank lines gone.

    Comments are dropped on purpose: this asks the code the runner executes, so a sentence
    that says the case checks something cannot stand in for the call that checks it.
    """
    with open(CASE, encoding="utf-8") as handle:
        lines = [line.rstrip() for line in handle]
    code = [line for line in lines if not line.lstrip().startswith("//")]
    text = "\n".join(code)
    marker = "func TestABootRefusedAtTheTransportTakesItsEventNamesAgain(t *testing.T) {"
    assert text.count(marker) == 1, "the case's test function is not the one function of its name"
    start = text.index(marker) + len(marker)
    end = text.index("\n}", start)
    return text[start:end]


def test_no_boot_that_is_meant_to_be_refused_is_skipped():
    """A skipped or duplicated ask is the way this case stops meaning anything quietly."""
    text = open(CASE, encoding="utf-8").read()
    assert "t.Skip" not in text, "the case skips, so neither half of the promise is measured"
    assert "testing.Short" not in text, "the case hides behind -short"
    asks = ASK.findall(body())
    assert len(asks) == 4, f"the case asks the claim {len(asks)} times, expected 4"
    starts = STARTS.findall(body())
    assert len(starts) == 2, f"the case boots the one App {len(starts)} times, expected 2"


def test_the_transport_constructor_refuses_only_the_first_boot():
    """One refusal between two boots is what puts the release inside Start."""
    code = body().replace("\n", " ")
    assert re.search(r"calls\+\+\s*if calls == 1 \{\s*return nil, errors\.New", code), (
        "the transport constructor no longer refuses exactly one boot, so no refusal runs "
        "inside Start and the case measures a Close instead"
    )


def test_the_three_asks_are_held_free_and_held_again_in_that_order():
    """New holds, the refused boot gives back, the retry takes again — one app, one shape."""
    # The first three asks name the declaration the composition does not hold; the fourth
    # names the one it does, and is what stops the third answer being about the second call.
    first, second, third, control = list(ASK.finditer(body()))
    # Held by New: a composition New built must already answer under its own names.
    assert first.group("when") == "==", "the ask after New no longer fails when the names are free"
    # Freed by the refusal: the failure is a refusal, which is what makes this the release.
    assert second.group("when") != "==", "the ask after the refused boot no longer fails on a refusal"
    # Taken again by the retry: a second boot that claimed nothing answers with no check.
    assert third.group("when") == "==", "the ask after the retry no longer fails when the names are free"
    # The same app and the same disagreeing declarations at all three, or the three asks
    # are about three different things and the order proves nothing.
    assert first.group("app") == second.group("app") == third.group("app"), "the asks name one app"
    assert first.group("decl") == second.group("decl") == third.group("decl"), (
        "the asks disagree with different declarations, so the last answer is not about the second boot"
    )
    assert control.group("decl") != first.group("decl"), (
        "the control ask names the same declaration as the three above, so nothing separates"
        " a refusal of the disagreeing payload from a refusal of the second boot"
    )


def test_the_retry_still_accepts_the_shape_its_own_module_declared():
    """Without this the third refusal could be about the second call rather than the payload."""
    code = body().replace("\n", " ")
    assert re.search(r"events\.CheckAppDeclared\(\s*[\w.]+\s*,\s*own\s*\)\s*;\s*err\s*!=\s*nil", code), (
        "the case no longer asks whether the composition it corrected still accepts what it"
        " declares, so any refusal at the retry would read as the defect it is looking for"
    )


def test_the_failure_each_release_step_prints_names_the_step_it_checked():
    """Each half refuses in its own words, so a red run says which call went missing."""
    code = body().replace("\n", " ")
    assert re.search(r"t\.Fatalf\(\"the boot the transport refused kept its names", code), (
        "the release half lost its own refusal, so a missing Runtime.Close release now reads"
        " as some other failure"
    )
    assert re.search(r"the boot after a transport refusal runs with no claim", code), (
        "the reacquire half lost its own refusal, so a missing claimDeclared in prepareRuntime"
        " now reads as some other failure"
    )


def test_the_case_holds_a_row_in_the_ratified_inventory():
    """A case nobody classified is a case the next prune can drop without anyone deciding it."""
    with open(os.path.join(ROOT, "tests", "inventory.json"), encoding="utf-8") as handle:
        rows = json.load(handle)["rows"]
    row = [r for r in rows if r.get("id") == CASE_ID]
    assert len(row) == 1, f"{CASE_ID} answers with {len(row)} rows in tests/inventory.json"
    assert row[0].get("verdict") == "keep", "the case is marked for something other than keeping"


if __name__ == "__main__":
    raise SystemExit(pytest.main([__file__, "-q"]))
