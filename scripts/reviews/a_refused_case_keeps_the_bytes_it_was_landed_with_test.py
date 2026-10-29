#!/usr/bin/env python3
"""A case that refused a delivery keeps the bytes it was landed with.

A conformance case is evidence about served behaviour only while nobody who is refused by it can
answer the refusal by rewriting the case. A case the refused author can edit is therefore no
evidence at all: the guard could be widened a line at a time — one assertion dropped, one shape
excused, one case skipped — until the delivery answers it with nothing about the behaviour changed.

The cases below carry this branch's own refusals, each by one commit, on paths nothing outside this
branch writes, so a figure recorded here moves only when one of them is rewritten. A figure is not a
taboo: a later check that means to replace a case states what every assertion still asks and records
its new digest in that same change, and a delivery never edits the case at all.

Run: python3 -m pytest -q \
    scripts/reviews/a_refused_case_keeps_the_bytes_it_was_landed_with_test.py
"""
import hashlib
import os

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

# Each case this branch's refusals rest on, with the digest it was landed with.
LANDED = {
    "scripts/reviews/resolved_components_no_other_answer_reads_as_a_records_forms_test.py":
        "0d023537efd2b9550488dbda408b05fd8927adcabbff97af526f1b904a056e6b",
    "scripts/reviews/resolved_forms_a_recorded_step_carries_every_field_the_form_answers_test.py":
        "bbb6909bfb306b9b3c3b42fed2935eb1350038f6a1890a2e51dd3d8a9aef7c8d",
    "apps/platformkit/a_records_stepped_form_is_not_read_as_a_fields_editability_test.go":
        "7e3dd320258f3bcae326ec22448fe538b2d5cebed7712af1cea5d7119b896140",
}


def digest(path):
    with open(os.path.join(ROOT, path), "rb") as handle:
        return hashlib.sha256(handle.read()).hexdigest()


def test_every_landed_case_is_still_the_file_it_was_landed_as():
    for path, wanted in sorted(LANDED.items()):
        assert os.path.exists(os.path.join(ROOT, path)), f"{path}: the case is gone from the tree"
        found = digest(path)
        assert found == wanted, (
            f"{path}: landed as {wanted}, the tree holds {found}. The bytes a refusal was written "
            "in are the refusal: whoever means to change this case says what every assertion "
            "still asks and records the new digest in that same change — a delivery never edits it")


def test_a_landed_case_still_asks_more_than_one_question():
    # The digest above catches a changed byte. This catches the shape a case is softened into —
    # one that loads, collects and asks nothing — should a digest be re-recorded beside that.
    for path in sorted(LANDED):
        with open(os.path.join(ROOT, path), encoding="utf-8") as handle:
            body = handle.read()
        asked = body.count("assert ") if path.endswith(".py") else body.count("t.Fatal")
        assert asked, f"{path}: it refuses nothing, so it is not a case"
        if path.endswith(".py"):
            assert body.count("\ndef test_") >= 2, (
                f"{path}: its {asked} assertions sit in one function, where a case of this "
                "record asks each question in a function of its own")
