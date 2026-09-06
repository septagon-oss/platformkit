import { createRequire, registerHooks } from 'node:module'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { correctSource, sdkVersion } from './corrections.mjs'

const require = createRequire(import.meta.url)
for (const name of ['core', 'fig', 'scene-graph']) {
  const manifest = resolve(dirname(require.resolve(`@open-pencil/${name}`)), '../package.json')
  const { version } = JSON.parse(readFileSync(manifest, 'utf8'))
  if (version !== sdkVersion) throw new Error(`Expected ${name}@${sdkVersion}; found ${version}`)
}

const coreRequire = createRequire(require.resolve('@open-pencil/core'))
const parserManifest = resolve(dirname(coreRequire.resolve('expr-eval')), '../package.json')
const parser = JSON.parse(readFileSync(parserManifest, 'utf8'))
if (parser.name !== 'expr-eval-fork' || parser.version !== '3.0.3') {
  throw new Error(`Expected expr-eval-fork@3.0.3; found ${parser.name}@${parser.version}`)
}

registerHooks({
  load(url, context, nextLoad) {
    const loaded = nextLoad(url, context)
    if (!url.startsWith('file:') || loaded.source == null) return loaded
    const source = typeof loaded.source === 'string' ? loaded.source : Buffer.from(loaded.source).toString()
    const corrected = correctSource(new URL(url).pathname, source)
    return corrected === null ? loaded : { ...loaded, source: corrected }
  },
})
