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
for (const [specifier, name, version] of [
  ['expr-eval', 'expr-eval-fork', '3.0.3'],
  ['pptxgenjs', '@neo-ma/pptxgenjs', '4.3.0'],
]) {
  const manifest = resolve(dirname(coreRequire.resolve(specifier)), '../package.json')
  const dependency = JSON.parse(readFileSync(manifest, 'utf8'))
  if (dependency.name !== name || dependency.version !== version) {
    throw new Error(`Expected ${name}@${version}; found ${dependency.name}@${dependency.version}`)
  }
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
