import assert from 'node:assert/strict'
import { test } from 'node:test'
import { sourceParagraph } from './paragraph-correction.mjs'

test('a failed replacement line releases each native paragraph exactly once', () => {
  const paragraphs = []
  const node = { text: 'one two', width: 4, textCase: 'ORIGINAL', textAlignHorizontal: 'LEFT', styleRuns: [], pluginData: [{
    pluginId: 'platformkit', key: 'platformkit.source', value: JSON.stringify({
      schema: 'platformkit.design-export.v1', textWrap: 'normal-v1',
    }),
  }] }
  assert.throws(() => sourceParagraph(node, value => {
    if (value.text === 'two') throw new Error('Line shaping failed')
    const paragraph = { deletions: 0, layout() {}, getLongestLine: () => value.text.trimEnd().length,
      getHeight: () => 1, delete() { this.deletions++ } }
    paragraphs.push(paragraph)
    return paragraph
  }), /Line shaping failed/)
  assert.equal(paragraphs.length, 3, 'the failure follows one completed line and a rejected combined line')
  assert.deepEqual(paragraphs.map(paragraph => paragraph.deletions), [1, 1, 1])
})
