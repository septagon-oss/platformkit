"""The browser runner receives both the mail and second-tenant addresses."""

import os
from pathlib import Path
import subprocess
import unittest


class JourneyEnvironment(unittest.TestCase):
    def test_mail_and_second_tenant_reach_the_same_browser_run(self):
        root = Path(__file__).resolve().parents[1]
        source = (root / "scripts/e2e.sh").read_text()
        start = source.index('PLATFORMKIT_E2E_URL="http://localhost:$port"')
        # Execute the actual final invocation, replacing only the process it
        # launches. Distinct values expose a lost assignment or crossed port.
        setup = r'''
set -eu
port=43101
second_port=43102
mail_port=43103
mail_url=http://localhost:43104
mailpit_url=http://localhost:43105
database=journey_fixture
password=first_fixture_password
second_password=second_fixture_password
output_args=(--output /unused)
npx() {
    printf '%s\n' "$PLATFORMKIT_E2E_URL" "$PLATFORMKIT_E2E_SECOND_URL" \
        "$PLATFORMKIT_E2E_MAIL_URL" "$PLATFORMKIT_MAIL_PORT" \
        "$PLATFORMKIT_E2E_MAILPIT_URL" "$@"
}
'''
        environment = {
            key: value for key, value in os.environ.items()
            if not key.startswith("PLATFORMKIT_")
        }
        result = subprocess.run(
            ["bash", "-c", setup + source[start:]],
            env=environment, stdin=subprocess.DEVNULL,
            capture_output=True, text=True, timeout=10,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.splitlines(), [
            "http://localhost:43101", "http://tenantb.localhost:43102",
            "http://localhost:43104", "43103", "http://localhost:43105",
            "playwright", "test", "--output", "/unused",
        ])


if __name__ == "__main__":
    unittest.main()
