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

registerHooks({
  load(url, context, nextLoad) {
    const loaded = nextLoad(url, context)
    if (!url.startsWith('file:') || loaded.source == null) return loaded
    const source = typeof loaded.source === 'string' ? loaded.source : Buffer.from(loaded.source).toString()
    const corrected = correctSource(new URL(url).pathname, source)
    return corrected === null ? loaded : { ...loaded, source: corrected }
  },
})
