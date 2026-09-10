// Test-only source and font fixtures; kept in the design-test budget.
import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { mkdtemp, writeFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

// Shared process setup, not an expected-result oracle. Every call runs the
// owning Go exporter with fresh inputs; assertions remain in the calling test.
export function exportCore(args = [], input) {
  return JSON.parse(execFileSync('go', ['run', './tools/designexport', ...args], {
    cwd: new URL('../../../../', import.meta.url), encoding: 'utf8', maxBuffer: 32 * 1024 * 1024,
    input: input === undefined ? undefined : JSON.stringify(input),
  }))
}

// Compile the caller's real Go composition once; every observation still runs
// it afresh with independent JSON input. The test owns source/binary cleanup,
// including build failures. Nothing is installed or cached in the checkout.
export async function sourceFixture(t, source) {
  const directory = await mkdtemp(join(tmpdir(), 'platformkit-design-source-'))
  t.after(() => rm(directory, { recursive: true, force: true }))
  const file = join(directory, 'main.go'), executable = join(directory, 'fixture')
  await writeFile(file, source, { flag: 'wx' })
  execFileSync('go', ['build', '-o', executable, file], { cwd: new URL('../../../../', import.meta.url) })
  return input => JSON.parse(execFileSync(executable, [], {
    encoding: 'utf8', maxBuffer: 32 * 1024 * 1024, input: JSON.stringify(input),
  }))
}

export function suppliedFonts(weights) {
  return weights.map(weight => {
    const bytes = readFileSync(new URL(`../node_modules/@fontsource/ibm-plex-sans/files/ibm-plex-sans-latin-${weight}-normal.woff`, import.meta.url))
    return { family: 'IBM Plex Sans', weight, style: 'normal', bytes, sha256: createHash('sha256').update(bytes).digest('hex') }
  })
}

// Return encoded source JSON as a string so the caller exercises its own
// lossless Scalar decoder instead of this fixture's ordinary JSON.parse.
export function sourceTokenFixture(t) {
  return sourceFixture(t, `package main
import (
  "encoding/json"
  "os"
  "slices"
  "github.com/septagon-oss/platformkit/design"
  "github.com/septagon-oss/platformkit/ui"
  "github.com/septagon-oss/platformkit/ui/style"
)
func main() {
  theme := design.Default()
  tokens, err := ui.ExportTokens(theme, "light", "dark")
  if err != nil { panic(err) }
  for i := range tokens.Modes {
    tokens.Modes[i].Fonts = nil
    tokens.Modes[i].Colors = slices.DeleteFunc(tokens.Modes[i].Colors, func(token design.Token) bool {
      return !slices.Contains([]string{"--pk-color-text-primary", "--pk-color-surface-canvas"}, token.Name)
    })
  }
  tokens.Colors = []design.ColorToken{
    {Name: "--selected-ink", Value: design.ColorValue{Reference: "--pk-color-text-primary"}},
    {Name: "--selected-mix", Value: design.ColorValue{Mix: &design.ColorMix{
      First: design.ColorValue{Reference: "--selected-ink"}, FirstPercent: 25,
      Second: design.ColorValue{Literal: "transparent"},
    }}},
  }
  tokens.Scales = []style.ScaleValue{
    {Scale: "spacing", Key: "1", Number: &style.Scalar{Value: json.Number("0.1000"), Unit: "px"}},
    {Scale: "leading", Key: "normal", Number: &style.Scalar{Value: json.Number("1.5"), Unit: ""}},
  }
  tokens.Shadows, tokens.Easings, tokens.Transitions = nil, nil, nil
  base, err := ui.Export(theme, nil)
  if err != nil { panic(err) }
  snapshot, err := base.WithTokens(tokens)
  if err != nil { panic(err) }
  if err := snapshot.CheckSourceContract("source-tokens.v1"); err != nil { panic(err) }
  encoded, err := json.Marshal(snapshot)
  if err != nil { panic(err) }
  if err := json.NewEncoder(os.Stdout).Encode(string(encoded)); err != nil { panic(err) }
}
`)
}

// The browser and shipped-editor checks exercise the same real constructor;
// neither replaces its paragraphs, constraints, optional slots or source props.
export function emptyStateFixture(t) {
  return sourceFixture(t, `package main
import (
  "encoding/json"
  "os"
  g "maragu.dev/gomponents"
  "github.com/septagon-oss/platformkit/design"
  "github.com/septagon-oss/platformkit/ui"
  "github.com/septagon-oss/platformkit/ui/components"
  "github.com/septagon-oss/platformkit/ui/css"
)
func main() {
  var input struct { Title, Description, Align, Constraint, Value string; Compact, Action bool }
  if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil { panic(err) }
  theme := design.Default()
  theme.Light.Typography.Display, theme.Dark.Typography.Display = "IBM Plex Sans", "IBM Plex Sans"
  var actions []g.Node
  if input.Action {
    action := components.ExampleOf(components.ExampleInfo{ID:"create", ComponentID:"pk-ui.component.button"}, components.ButtonProps{Label:"Create album"}, components.Button)
    actions = []g.Node{action.Node}
  }
  example := components.ExampleWithSlots(components.ExampleInfo{ID:"fixture/empty", ComponentID:"pk-ui.component.emptystate"},
    components.EmptyStateProps{ComponentProps:components.ComponentProps{ID:"empty"}, Title:input.Title, Description:input.Description, Compact:input.Compact, Bordered:true},
    components.EmptyStateSlots{Actions:actions}, components.EmptyStateWithSlots)
  sheet := css.NewSheet()
  if input.Align != "" { sheet.Select("#empty", css.Decl("align-items", css.Literal(input.Align))) }
  if input.Constraint != "" { sheet.Select("#empty > p", css.Decl(input.Constraint, css.Literal(input.Value))) }
  snapshot, err := ui.Export(theme, []components.Example{example}, ui.Extra{Sheets:[]*css.Sheet{sheet}})
  if err != nil { panic(err) }
  if err := json.NewEncoder(os.Stdout).Encode(snapshot); err != nil { panic(err) }
}
`)
}
