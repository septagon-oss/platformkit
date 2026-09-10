import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { writeFile, mkdtemp, rm, access, realpath } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { tmpdir } from 'node:os';
import { dirname, basename, resolve, relative, isAbsolute, sep } from 'node:path';
import { fileURLToPath } from 'node:url';
import { text } from 'node:stream/consumers';

// Build only the snapshot supplied by the composition owner. Generated stories,
// caches and output stay outside the checkout, and no development server is exposed.
const here = dirname(fileURLToPath(import.meta.url));
const repository = resolve(here, '../..');
const bridge = resolve(here, '../assets/js/storybook.js');
const output = process.argv[2] && resolve(process.argv[2]);
if (!output || !isAbsolute(process.argv[2])) {
  throw new Error('Usage: npm run build -- /absolute/path/outside/checkout < selected-export.json');
}
const destination = relative(repository, resolve(await realpath(dirname(output)), basename(output)));
if (destination !== '..' && !destination.startsWith('..' + sep) && !isAbsolute(destination)) {
  throw new Error('Storybook output must stay outside the checkout.');
}
try {
  await access(output);
  throw new Error('The output directory already exists; choose a new build directory.');
} catch (error) {
  if (error.code !== 'ENOENT') throw error;
}
const snapshot = JSON.parse(await text(process.stdin), (_key, value, context) =>
  typeof value === 'number' && !Number.isSafeInteger(value) && /^-?\d+$/.test(context.source)
    ? JSON.rawJSON(context.source) : value);
if (snapshot.schema !== 'platformkit.design-export.v1' || !/^[a-f0-9]{64}$/.test(snapshot.sha256) || !snapshot.examples.length) {
  throw new Error('Expected a nonempty v1 ui.Export snapshot with its SHA-256.');
}
const work = await mkdtemp(resolve(tmpdir(), 'platformkit-storybook-'));
const require = createRequire(import.meta.url);
function hasLargeInteger(value) {
  return JSON.isRawJSON(value) || value !== null && typeof value === 'object' && Object.values(value).some(hasLargeInteger);
}
try {
  const seen = new Set();
  for (const example of snapshot.examples) {
    if (!example.id || seen.has(example.id)) throw new Error('Missing or duplicate example ID');
    seen.add(example.id);
    const args = {};
    const argTypes = {};
    const rawFields = [];
    for (const [name, schema] of Object.entries(example.schema?.properties || {})) {
      const value = example.props[name] ?? schema.default;
      const large = hasLargeInteger(value);
      if (large) rawFields.push(name);
      if (value !== undefined) args[name] = large ? JSON.stringify(value) : value;
      const type = Array.isArray(schema.type) ? schema.type.find(value => value !== 'null') : schema.type;
      const control = large ? 'text' : schema.enum ? 'select' :
        ({ boolean: 'boolean', integer: 'number', number: 'number', string: 'text' }[type] || 'object');
      argTypes[name] = { control: { type: control }, options: schema.enum, description: schema.description,
        table: { type: { summary: large ? 'exact JSON integer' : type || 'JSON' } } };
    }
    const names = example.name.split(' / ');
    const id = example.id.replace(/[^a-zA-Z0-9-]/g, '-').toLowerCase() + '-' + createHash('sha256').update(example.id).digest('hex').slice(0, 8);
    const title = [example.group, names[0]].filter(Boolean).join('/');
    const parameters = { platformkit: { example: example.id, rawFields } };
    // CSF's static indexer needs literal title/id properties, not computed metadata.
    const meta = `{ id: ${JSON.stringify(id)}, title: ${JSON.stringify(title)}, args: ${JSON.stringify(args)},
      argTypes: ${JSON.stringify(argTypes)}, parameters: ${JSON.stringify(parameters)} }`;
    await writeFile(resolve(work, `${id}.stories.js`),
      `import { render } from ${JSON.stringify(bridge)};\n` +
      `export default ${meta};\nexport const Example = { name: ${JSON.stringify(names.slice(1).join(' / ') || 'Default')}, render };\n`);
  }
  const framework = dirname(require.resolve('@storybook/html-vite/package.json'));
  await writeFile(resolve(work, 'main.js'), `export default {
    stories: ['./*.stories.js'], framework: ${JSON.stringify(framework)},
    core: { disableTelemetry: true },
    async viteFinal(config) {
      config.cacheDir = ${JSON.stringify(resolve(work, 'cache'))};
      config.build.sourcemap = false;
      return config;
    }
  };\n`);
  await writeFile(resolve(work, 'preview.js'), `export { default } from ${JSON.stringify(bridge)};\n`);
  const pkg = require('storybook/package.json');
  const cli = resolve(dirname(require.resolve('storybook/package.json')), typeof pkg.bin === 'string' ? pkg.bin : pkg.bin.storybook);
  execFileSync(process.execPath, [cli, 'build', '--config-dir', work, '--output-dir', output, '--disable-telemetry'],
    { cwd: here, stdio: ['ignore', 'inherit', 'inherit'], env: { ...process.env, STORYBOOK_DISABLE_TELEMETRY: '1' } });
  await writeFile(resolve(output, 'platformkit.json'), JSON.stringify({ sha256: snapshot.sha256 }));
} finally {
  await rm(work, { recursive: true, force: true });
}
