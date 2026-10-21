"""Only complete, successful design reports satisfy the generated-page assertion."""
from pathlib import Path
import subprocess
import unittest


ROOT = Path(__file__).resolve().parents[2]


class ReferenceGateReports(unittest.TestCase):
    def test_missing_incomplete_and_refused_reports_are_rejected(self):
        program = r"""
const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { createRequire, stripTypeScriptTypes } = require('node:module');
const { join } = require('node:path');
const root = process.argv[1];
const local = createRequire(join(root, 'e2e/package.json'));
const { expect } = local('@playwright/test');
const source = readFileSync(join(root, 'e2e/generated-pages-design-gate.spec.ts'), 'utf8');
const start = source.indexOf('function verdict(');
const end = source.indexOf("test('the generated pages", start);
assert.ok(start >= 0 && end > start, 'the spec must declare its report assertions');
const body = stripTypeScriptTypes(source.slice(start, end));
const cases = [
  { name: 'complete', text: 'design gate: ok (2 width(s))\n', status: 0 },
  { name: 'silent error', text: '', status: 2, reason: /the gate did not judge/ },
  { name: 'one width', text: 'design gate: ok (1 width(s))\n', status: 0,
    reason: /the gate did not judge/ },
  { name: 'measurements without verdict', text: '390px edges=[24,97]\n1440px edges=[280,1158]\n',
    status: 0, reason: /the gate did not judge/ },
  { name: 'refusal', text: 'DESIGN GATE FAILED\n - 390px: 3 left edges\n', status: 1,
    reason: /fails the design gate/ },
  { name: 'refusal beside success marker',
    text: ' - 1440px: body measure up to 76ch\ndesign gate: ok (2 width(s))\n', status: 0,
    reason: /fails the design gate/ },
];
for (const scenario of cases) {
  const execute = () => {
    if (scenario.status) throw Object.assign(new Error('gate exited'), { stdout: scenario.text });
    return scenario.text;
  };
  const api = new Function('execFileSync', 'join', 'gate', 'shortTmp', 'console', 'expect',
    body + '\nreturn { verdict, expectPasses };')(execute, join, '', {}, { log() {} }, expect);
  const report = api.verdict('generated list', []);
  const judge = () => api.expectPasses('generated list', report);
  if (scenario.reason) assert.throws(judge, scenario.reason, scenario.name);
  else assert.doesNotThrow(judge, scenario.name);
  process.stdout.write(scenario.name + ': ' + (scenario.reason ? 'rejected' : 'accepted') + '\n');
}
"""
        result = subprocess.run(
            ["node", "--disable-warning=ExperimentalWarning", "-e", program, str(ROOT)],
            cwd=ROOT, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT, text=True, timeout=30,
        )
        self.assertEqual(result.returncode, 0, result.stdout)
        self.assertEqual(len(result.stdout.splitlines()), 6, result.stdout)


if __name__ == "__main__":
    unittest.main()
