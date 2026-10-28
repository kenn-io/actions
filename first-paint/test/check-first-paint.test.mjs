// @ts-check
import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { mkdtemp, readFile, writeFile } from "node:fs/promises";
import { createServer } from "node:http";
import { tmpdir } from "node:os";
import path from "node:path";
import { test } from "node:test";
import { promisify } from "node:util";
import { check } from "../check-first-paint.mjs";

const CHECK = new URL("../check-first-paint.mjs", import.meta.url).pathname;
const HOLD = await readFile(new URL("../hold-first-paint.js", import.meta.url), "utf8");
const EMBEDDED = `@font-face { font-family: Face; src: url("data:font/woff2;base64,AAAA") format("woff2"); }`;
const holdScript = `<script>// A copy for this site.\n${HOLD}</script>`;

/** @param {{ head?: string, body?: string }} page */
const html = ({ head = `${holdScript}<link rel="stylesheet" href="/site.css">`, body = "<p>Text</p>" }) =>
  `<!doctype html><html><head><meta charset="utf-8">${head}</head><body>${body}</body></html>`;

/** @param {Record<string, string>} files */
async function site(files) {
  const dir = await mkdtemp(path.join(tmpdir(), "first-paint-"));
  for (const [name, content] of Object.entries(files)) await writeFile(path.join(dir, name), content);
  return dir;
}

test("a page that holds its first paint and embeds its fonts passes", async () => {
  const dist = await site({
    "index.html": html({
      head: `${holdScript}<link rel="stylesheet" href="site.css"><link rel="stylesheet" media="print" href="/print.css">`,
    }),
    "site.css": `@import "base.css"; ${EMBEDDED}`,
    "base.css": `/* @font-face { src: url(late.woff2) } */ @font-face { font-family: Base; src: url('data:font/woff2;base64,AAAA'); }`,
    "print.css": `@font-face { font-family: Print; src: url(/print.woff2); }`,
  });
  assert.deepEqual(await check({ dist }), new Map());
});

test("a page with no stylesheets needs no hold script", async () => {
  const dist = await site({ "index.html": html({ head: "" }) });
  assert.deepEqual(await check({ dist }), new Map());
});

const broken = [
  { name: "no hold script", head: `<link rel="stylesheet" href="/site.css">`, expect: /does not inline/ },
  {
    name: "an edited hold script",
    head: `${holdScript.replace("3000", "9000")}<link rel="stylesheet" href="/site.css">`,
    expect: /does not inline/,
  },
  { name: "hold script after a stylesheet", head: `<link rel="stylesheet" href="/site.css">${holdScript}`, expect: /comes after/ },
  { name: "hold script after an inline style", head: `<style>p {}</style>${holdScript}`, expect: /comes after/ },
  { name: "a font file", css: `@font-face { font-family: Face; src: url(/face.woff2) format("woff2"); }`, expect: /\/face\.woff2 .*separate file/ },
  { name: "a font file in an imported stylesheet", css: `@import url("fonts.css");`, fonts: `@font-face { src: url(face.woff2); }`, expect: /face\.woff2 in \/fonts\.css/ },
  { name: "a font file in an inline style", head: `${holdScript}<style>@font-face { src: url(face.woff2); }</style>`, expect: /separate file/ },
  {
    name: "a stylesheet swapped in on load",
    head: `${holdScript}<link rel="stylesheet" href="/site.css" media="print" onload="this.media='all'">`,
    expect: /onload/,
  },
  {
    name: "a preload swapped in on load",
    head: `${holdScript}<link rel="preload" as="style" href="/site.css" onload="this.rel='stylesheet'">`,
    expect: /onload/,
  },
  { name: "a stylesheet in the body", body: `<link rel="stylesheet" href="/site.css">`, head: holdScript, expect: /outside <head>/ },
  { name: "a stylesheet on another origin", head: `${holdScript}<link rel="stylesheet" href="https://fonts.example/css">`, expect: /another origin/ },
  { name: "a missing stylesheet", head: `${holdScript}<link rel="stylesheet" href="/gone.css">`, expect: /not found/ },
];

for (const { name, head, body, css = EMBEDDED, fonts = "", expect } of broken) {
  test(`a page fails with ${name}`, async () => {
    const dist = await site({ "index.html": html({ head, body }), "site.css": css, "fonts.css": fonts });
    const problems = (await check({ dist })).get("index.html") ?? [];
    assert.match(problems.join("\n"), expect);
  });
}

test("pages a server renders are checked over HTTP", async (t) => {
  const pages = {
    "/": html({}),
    "/site.css": `@font-face { src: url(/face.woff2); }`,
  };
  const server = createServer((request, response) => {
    const body = pages[/** @type {keyof typeof pages} */ (request.url ?? "")];
    response.writeHead(body === undefined ? 404 : 200).end(body);
  });
  await new Promise((done) => server.listen(0, "127.0.0.1", () => done(undefined)));
  t.after(() => server.close());
  const address = server.address();
  assert.ok(address && typeof address === "object");
  const url = `http://127.0.0.1:${address.port}/`;
  assert.match((await check({ urls: [url] })).get(url)?.join("\n") ?? "", /face\.woff2 .*separate file/);
});

test("the command exits 1 and names the page and problem", async () => {
  const dist = await site({ "index.html": html({ head: `<link rel="stylesheet" href="/site.css">` }), "site.css": EMBEDDED });
  const run = promisify(execFile)(process.execPath, [CHECK, "--dist", dist]);
  await assert.rejects(run, (/** @type {any} */ error) => error.code === 1 && /index\.html: <head> does not inline/.test(error.stderr));
});
