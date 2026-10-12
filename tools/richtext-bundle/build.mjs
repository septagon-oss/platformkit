// Builds ui/assets/vendor/richtext/editor.js and its LICENSE.md from the exact
// dependency tree package-lock.json pins.
//
// Two promises, both of which `make check` asserts:
//
//   Reproducible. The same lockfile produces the same bytes: esbuild's own
//   minifier is the only one in the pipeline, the target and the defines are
//   fixed here rather than inherited from the environment, no timestamp and no
//   path of the build machine reaches the output, and upstream notices go in
//   the licence file instead of being duplicated into the bundle.
//
//   Honest about its licences. LICENSE.md is generated from the packages whose
//   code esbuild actually put in the bundle — read off the build's own metafile,
//   not off a hand-maintained list — so a dependency added to entry.js arrives
//   with its notice in the same build.
import { build } from "esbuild";
import { gunzipSync, gzipSync } from "node:zlib";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { basename, dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const repo = resolve(here, "..", "..");
// Built in place by default; an output directory argument builds beside the
// committed file instead, which is how the check compares the two without
// touching the working tree.
const outDir = process.argv[2] ? resolve(process.argv[2]) : join(repo, "ui", "assets", "vendor", "richtext");
mkdirSync(outDir, { recursive: true });
const bundlePath = join(outDir, "editor.js");
const licencePath = join(outDir, "LICENSE.md");

const result = await build({
  absWorkingDir: here,
  bundle: true,
  define: { "process.env.NODE_ENV": '"production"' },
  entryPoints: ["entry.js"],
  format: "esm",
  legalComments: "none",
  metafile: true,
  minify: true,
  outfile: bundlePath,
  platform: "browser",
  target: "es2022",
});

// One section per package whose code is in the output, keyed by the package
// name so the order does not depend on the order esbuild walked the graph.
const packages = new Map();
const outputs = result.metafile.outputs;
const output = outputs[bundlePath] ?? Object.values(outputs)[0];
for (const input of Object.keys(output.inputs)) {
  if (!input.startsWith("node_modules/")) continue;
  const parts = input.split("/");
  const name = parts[1].startsWith("@") ? `${parts[1]}/${parts[2]}` : parts[1];
  packages.set(name, join(here, "node_modules", ...name.split("/")));
}

const notices = [];
for (const [name, dir] of [...packages].sort(([a], [b]) => (a < b ? -1 : 1))) {
  const file = ["LICENSE.md", "LICENSE"].map((f) => join(dir, f)).find((f) => existsSync(f));
  if (!file) throw new Error(`${name} ships no licence file; the bundle cannot be redistributed without it`);
  notices.push(`## ${name}\n\n${readFileSync(file, "utf8").trim()}\n`);
}

writeFileSync(
  licencePath,
  [
    "# Licences for the rich text editor bundle",
    "",
    "`editor.js` beside this file bundles the packages below, each under its own",
    "licence, whose full text follows. The build script writes this file from the",
    "licence each installed package carries; do not edit it here.",
    "",
    ...notices,
  ].join("\n"),
);

const bytes = readFileSync(bundlePath);
const gzipped = gzipSync(bytes, { level: 9 });
if (gunzipSync(gzipped).length !== bytes.length) throw new Error("gzip round trip disagree with itself");
console.log(
  `richtext bundle: ${bytes.length} bytes, ${gzipped.length} gzipped (ceiling 153600), ` +
    `${packages.size} packages, ${notices.length} notices`,
);
