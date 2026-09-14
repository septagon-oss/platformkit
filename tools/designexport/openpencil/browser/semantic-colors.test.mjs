import assert from 'node:assert/strict'
import { test } from 'node:test'
import { chromium } from 'playwright'
import { sourceFixture } from './fixtures.test.mjs'

test('source-owned colour resolution agrees with browser CSS in both modes and with translucent inputs', async t => {
  const source = await sourceFixture(t, `package main
import (
  "encoding/json"
  "os"
  "github.com/septagon-oss/platformkit/design"
  "github.com/septagon-oss/platformkit/ui/style"
)
func main() {
  var input struct { Accent, Sidebar string }
  if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil { panic(err) }
  pair := design.Default()
  if input.Accent != "" { pair.Light.AccentDefault, pair.Dark.AccentDefault = input.Accent, input.Accent }
  if input.Sidebar != "" { pair.Light.SidebarBg, pair.Dark.SidebarBg = input.Sidebar, input.Sidebar }
  roles := style.RoleColors()
  extra := []design.ColorToken{
    {Name:"--fixture-zero", Value:design.ColorValue{Mix:&design.ColorMix{
      First:design.ColorValue{Literal:"#ff000000"}, FirstPercent:50, Second:design.ColorValue{Literal:"#0000ff00"}}}},
    {Name:"--fixture-alpha", Value:design.ColorValue{Mix:&design.ColorMix{
      First:design.ColorValue{Literal:"#ff000088"}, FirstPercent:25, Second:design.ColorValue{Literal:"#00ff0044"}}}},
  }
  sheet := style.RoleVars()
  for _, token := range extra {
    value, err := token.Value.CSS()
    if err != nil { panic(err) }
    sheet.Var(token.Name[2:], value)
  }
  roles = append(roles, extra...)
  var out struct { CSS string; Modes map[string]map[string]design.SRGBA }
  out.CSS = style.ThemeVars(pair.Light, pair.Dark).CSS() + sheet.CSS()
  out.Modes = make(map[string]map[string]design.SRGBA)
  for _, theme := range pair.Both() {
    values, err := design.ResolveColors(theme.Tokens(), roles)
    if err != nil { panic(err) }
    out.Modes[theme.Name] = values
  }
  if err := json.NewEncoder(os.Stdout).Encode(out); err != nil { panic(err) }
}
`)
  const browser = await chromium.launch({ headless: true })
  t.after(() => browser.close())
  for (const input of [{}, { Accent: '#11223388', Sidebar: '#ff000080' }]) {
    const fixture = source(input)
    for (const preference of ['light', 'dark']) for (const mode of [null, 'light', 'dark']) {
      const expected = fixture.Modes[mode ?? preference]
      const page = await browser.newPage({ colorScheme: preference })
      try {
        await page.setContent(`<html${mode ? ` data-theme="${mode}"` : ''}><style>${fixture.CSS}</style><body><span id="probe"></span></body></html>`)
        const observed = await page.evaluate(names => {
          const probe = document.querySelector('#probe')
          return Object.fromEntries(names.map(name => {
            // A full-weight sRGB mix requests one comparable browser serialization
            // without implementing its interpolation or quantizing through canvas.
            probe.style.color = `color-mix(in srgb, var(${name}) 100%, transparent)`
            return [name, getComputedStyle(probe).color]
          }))
        }, Object.keys(expected))
        for (const [name, channels] of Object.entries(expected)) {
          assert.match(observed[name], /^color\(srgb /, `${name}: browser did not compute the expression`)
          const [rgb, alpha = '1'] = observed[name].slice(11, -1).split('/').map(part => part.trim())
          const actual = [...rgb.split(/\s+/).map(Number), Number(alpha)]
          assert.equal(actual.length, 4)
          for (let i = 0; i < 4; i++) assert.ok(Math.abs(actual[i] - channels[i]) < 0.00001,
            `${mode ?? preference}/${name}/${i}: browser ${actual[i]}, source ${channels[i]}`)
        }
      } finally { await page.close() }
    }
  }
})
