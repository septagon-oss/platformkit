#!/usr/bin/env python3
"""A second application composition leaves the first app's event contract intact."""

import subprocess


def test_app_event_catalogues_keep_their_own_payload_contracts():
    result = subprocess.run(
        [
            "go",
            "test",
            "./kit/app",
            "-run",
            "^TestTwoAppCompositionsKeepTheirEventPayloadContracts$",
            "-count=1",
        ],
        check=False,
        capture_output=True,
        text=True,
        timeout=900,
    )
    assert result.returncode == 0, result.stdout + result.stderr
