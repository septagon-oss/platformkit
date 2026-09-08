import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { mkdtemp, writeFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { test } from 'node:test'
import { chromium } from 'playwright'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { buildFoundation } from '../foundation.mjs'

test('configured typography agrees across source theme cascade and native token modes', async t => {
  const directory = await mkdtemp(join(tmpdir(), 'platformkit-typography-'))
  t.after(() => rm(directory, { recursive: true, force: true }))
  const file = join(directory, 'main.go')
  await writeFile(file, `package main
import (
  "encoding/json"
  "os"
  "github.com/septagon-oss/platformkit/design"
  "github.com/septagon-oss/platformkit/ui"
  "github.com/septagon-oss/platformkit/ui/components"
)
func main() {
  var input struct { Light, Dark design.Typography }
  if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil { panic(err) }
  theme := design.Default()
  theme.Light.Typography, theme.Dark.Typography = input.Light, input.Dark
  snapshot, err := ui.Export(theme, components.Gallery())
  if err != nil { panic(err) }
  if err := json.NewEncoder(os.Stdout).Encode(snapshot); err != nil { panic(err) }
}
`, { flag: 'wx' })
  const configured = { light: { display: '"Customer Display", serif', body: '"Customer Body", sans-serif' },
    dark: { display: '"Alternate Display", serif', body: '"Alternate Body", sans-serif', mono: '"Customer Mono", monospace' } }
  const snapshot = JSON.parse(execFileSync('go', ['run', file], { cwd: new URL('../../../../', import.meta.url),
    encoding: 'utf8', maxBuffer: 32 * 1024 * 1024, input: JSON.stringify(configured) }))
  const browser = await chromium.launch({ headless: true })
  try {
    for (const preference of ['light', 'dark']) for (const attribute of [null, 'light', 'dark']) {
      const page = await browser.newPage({ colorScheme: preference })
      try {
        await page.setContent(`<html${attribute ? ` data-theme="${attribute}"` : ''}><head><style>${snapshot.css}</style></head><body></body></html>`)
        const tokens = snapshot.themes.find(theme => theme.mode === (attribute ?? preference)).tokens.filter(token => token.type === 'fontFamily')
        for (const token of tokens) assert.equal(await page.evaluate(name =>
          getComputedStyle(document.documentElement).getPropertyValue(name).trim(), token.name), token.value)
      } finally { await page.close() }
    }
    let { graph } = buildFoundation(snapshot)
    for (let cycle = 0; cycle < 3; cycle++) {
      const collection = [...graph.variableCollections.values()].find(item => item.name === 'Foundation')
      for (const theme of snapshot.themes) {
        const mode = collection.modes.find(item => item.name === theme.mode)
        for (const role of ['display', 'body', 'mono']) {
          const variable = graph.getVariablesForCollection(collection.id).find(item => item.name === `--pk-font-${role}`)
          assert.equal(variable.type, 'STRING')
          const expected = configured[theme.mode][role] ?? '"IBM Plex Mono", "SFMono-Regular", Consolas, monospace'
          assert.equal(variable.valuesByMode[mode.modeId], expected, `${theme.mode}/${role}/save ${cycle}`)
        }
      }
      if (cycle < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
    }
    assert.equal(browser.contexts().length, 0)
  } finally { await browser.close() }
})
