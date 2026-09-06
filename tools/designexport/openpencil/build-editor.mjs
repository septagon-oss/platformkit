import { createHash } from 'node:crypto'
import { createRequire } from 'node:module'
import { copyFileSync, mkdirSync, readFileSync, readdirSync, realpathSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'
import { corrections, correctSource, sdkVersion } from './corrections.mjs'

// Run only in the disposable Docker build stage. Application sources come
// from the checksum-pinned archive; engine modules come from our npm lock.
const adapter = dirname(fileURLToPath(import.meta.url))
const upstream = realpathSync(process.argv[2])
const require = createRequire(import.meta.url)
const upstreamRequire = createRequire(join(upstream, 'package.json'))
const sha256 = bytes => createHash('sha256').update(bytes).digest('hex')
const engine = /^@open-pencil\/(core|fig|scene-graph|pen|kiwi)(\/.*)?$/
const engineRoot = /\/packages\/(core|fig|scene-graph|pen|kiwi)\//
const pinned = specifier => require.resolve(specifier)
const manifest = JSON.parse(readFileSync(join(upstream, 'package.json'), 'utf8'))
if (manifest.version !== sdkVersion) throw new Error('Upstream app and corrected SDK versions differ')
for (const name of ['core', 'fig', 'scene-graph', 'pen', 'kiwi']) {
  const path = join(dirname(pinned(`@open-pencil/${name}`)), '../package.json')
  if (JSON.parse(readFileSync(path, 'utf8')).version !== sdkVersion) {
    throw new Error(`Unexpected native package version: ${name}`)
  }
}
const coreRequire = createRequire(pinned('@open-pencil/core'))
for (const [specifier, name, version] of [
  ['expr-eval', 'expr-eval-fork', '3.0.3'], ['pptxgenjs', '@neo-ma/pptxgenjs', '4.3.0'],
]) {
  const dependency = JSON.parse(readFileSync(join(dirname(coreRequire.resolve(specifier)), '../package.json'), 'utf8'))
  if (dependency.name !== name || dependency.version !== version) throw new Error(`Unreviewed browser dependency: ${specifier}`)
}

function replaceOnce(source, before, after) {
  if (source.split(before).length !== 2) throw new Error(`Browser build anchor changed: ${before}`)
  return source.replace(before, after)
}

// Keep upstream's UI configuration, excluding desktop/dev automation and PWA
// installation. Neither belongs in this static, digest-promoted browser image.
let configSource = readFileSync(join(upstream, 'vite.config.ts'), 'utf8')
for (const text of [
  "import { localAutomationToken, openPencilAutomationPlugin } from './vite/automation'\n",
  "import { openPencilPwaPlugin } from './vite/pwa'\n",
  '    openPencilAutomationPlugin(command, host),\n',
  '    openPencilPwaPlugin()\n',
]) configSource = replaceOnce(configSource, text, '')
configSource = replaceOnce(configSource, 'JSON.stringify(localAutomationToken(command))', 'JSON.stringify(null)')
const configFile = join(upstream, 'platformkit.vite.config.ts')
writeFileSync(configFile, configSource, { flag: 'wx' })
const { build, loadConfigFromFile } = await import(pathToFileURL(upstreamRequire.resolve('vite')))
const { config } = await loadConfigFromFile({ command: 'build', mode: 'production' }, configFile)

const seen = new Set()
function nativeBoundary() {
  return {
    name: 'platformkit-native-boundary', enforce: 'pre',
    resolveId(specifier) {
      return engine.test(specifier) ? pinned(specifier) : null
    },
    load(id) {
      const path = id.split('?')[0]
      if (path.startsWith(upstream + '/') && engineRoot.test(path)) {
        throw new Error(`Upstream engine source bypasses the corrected SDK: ${path}`)
      }
      if (!path.startsWith(adapter + '/node_modules/') || !path.endsWith('.js') && !path.endsWith('.mjs')) return null
      const source = readFileSync(path, 'utf8')
      let result = correctSource(path, source)
      if (result !== null) seen.add(path.slice(adapter.length + '/node_modules/'.length))
      // The published JS distribution retains two TypeScript worker URLs.
      // Correct them before Vite discovers worker entries, in both builds.
      if (path.endsWith('/@open-pencil/core/dist/io/formats/fig/read.js')) {
        if (sha256(source) !== '137b1c1619c6841a04085157eaebca5b7c2e8a894257892abd647cc6d9eafe80') {
          throw new Error('FIG reader source changed')
        }
        result = replaceOnce(source, 'parse/worker.ts', 'parse/worker.js')
      }
      if (path.endsWith('/@open-pencil/core/dist/io/formats/fig/export.js')) {
        result = replaceOnce(result, './export-worker.ts', './export-worker.js')
      }
      return result === null ? null : { code: result, map: null }
    },
    transform(source, id) {
      if (id !== join(upstream, 'src/main.ts')) return null
      if (sha256(source) !== 'ba0318dd65f3cbadaa406d01655b7190c5e1cfc55c5e92b33335d0fd1e5b8bbc') {
        throw new Error('Browser entry source changed')
      }
      return { code: replaceOnce(source, source.slice(source.indexOf('if (!IS_TAURI) {')), ''), map: null }
    },
  }
}

// Use Node's package-export resolver, not a second subpath mapping. Vue and
// dom-css stay upstream UI sources; every engine reference resolves once.
const aliases = config.resolve.alias.filter(alias => {
  const replacement = alias.replacement
  return !engineRoot.test(replacement) && alias.find !== 'opentype.js'
})
config.resolve.alias = [
  { find: 'opentype.js', replacement: pinned('opentype.js') },
  ...aliases,
]
config.plugins.unshift(nativeBoundary())
config.worker = { plugins: () => [nativeBoundary()] }
await build({ ...config, configFile: false, root: upstream, build: { ...config.build, reportCompressedSize: false } })

// Missing transforms are a build failure, not a silently less-correct editor.
// CommonJS expression code is tested by Node; the browser selects its ESM entry.
for (const path of Object.keys(corrections).filter(path => !path.endsWith('/bundle.js'))) {
  if (!seen.has(path)) throw new Error(`Browser omitted a required native correction: ${path}`)
}
const licenses = join(upstream, 'dist/licenses')
mkdirSync(licenses, { recursive: true })
for (const [source, name] of [
  [join(upstream, 'LICENSE'), 'OpenPencil-LICENSE'],
  [join(adapter, 'LICENSE'), 'PlatformKit-LICENSE'], [join(adapter, 'NOTICE'), 'PlatformKit-NOTICE'],
]) copyFileSync(source, join(licenses, name))
const inputs = Object.fromEntries(readdirSync(adapter).filter(name => name.endsWith('.mjs') ||
  ['package.json', 'package-lock.json', 'Dockerfile', 'nginx.conf', 'LICENSE', 'NOTICE'].includes(name))
  .sort().map(name => [name, sha256(readFileSync(join(adapter, name)))]))
writeFileSync(join(upstream, 'dist/platformkit-provenance.json'), JSON.stringify({
  schema: 'platformkit.openpencil.provenance.v1',
  upstream: { version: sdkVersion, commit: 'c29654cd07ac46b53e76c16b18505919f16571be' },
  adapter: { inputs, correctedModules: [...seen].sort() },
  upstreamLockSHA256: sha256(readFileSync(join(upstream, 'bun.lock'))),
  designProfile: null, defaultDocument: null,
  scope: 'generic-editor-without-packaged-design',
}, null, 2) + '\n', { flag: 'wx' })
