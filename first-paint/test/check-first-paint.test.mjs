// @ts-check
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { cp, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { createServer } from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { after, before, describe, it } from "node:test";

const CLI = new URL("../check-first-paint.mjs", import.meta.url).pathname;
const FONT = new URL("fixtures/ibm-plex-mono-400.woff2", import.meta.url).pathname;
const HOLD = new URL("../hold-first-paint.js", import.meta.url).pathname;
const BODY = `<p style="font-family: Fixture, monospace">The quick brown fox</p>`;

/**
 * @param {string} head
 * @returns {string}
 */
const page = (head) => `<!doctype html><html><head><meta charset="utf-8">${head}</head><body>${BODY}</body></html>`;

/**
 * @param {string} src
 * @returns {string}
 */
const fontFace = (src) => `@font-face { font-family: Fixture; font-display: swap; src: url("${src}") format("woff2"); }`;

/**
 * Runs the checker and collects its output.
 * @param {string[]} args
 * @returns {Promise<{ code: number | null, stdout: string, stderr: string }>}
 */
function run(args) {
  return new Promise((done) => {
    const child = spawn(process.execPath, [CLI, ...args]);
    let stdout = "";
    let stderr = "";
    child.stdout.on("data", (chunk) => (stdout += chunk));
    child.stderr.on("data", (chunk) => (stderr += chunk));
    child.on("close", (code) => done({ code, stdout, stderr }));
  });
}

/**
 * Writes a site with one page and the given extra files.
 * @param {string} root
 * @param {string} name
 * @param {string} html
 * @param {Record<string, string>} files
 * @returns {Promise<string>}
 */
async function site(root, name, html, files = {}) {
  const dir = join(root, name);
  await cp(FONT, join(dir, "font.woff2"));
  await writeFile(join(dir, "index.html"), html);
  await Promise.all(Object.entries(files).map(([file, body]) => writeFile(join(dir, file), body)));
  return dir;
}

describe("first-paint", () => {
  /** @type {string} */
  let root;
  /** @type {string} */
  let embedded;
  /** @type {string} */
  let held;

  before(async () => {
    root = await mkdtemp(join(tmpdir(), "first-paint-"));
    const data = `data:font/woff2;base64,${(await readFile(FONT)).toString("base64")}`;
    embedded = await site(root, "embedded", page(`<link rel="stylesheet" href="/site.css">`), {
      "site.css": fontFace(data),
    });
    // Embeds its font and inlines the hold script, as the README tells a UI to do.
    const hold = `<script>${await readFile(HOLD, "utf8")}</script><link rel="stylesheet" href="/site.css">`;
    held = await site(root, "held", page(hold), { "site.css": fontFace(data) });
  });

  after(() => rm(root, { recursive: true, force: true }));

  it("passes a page that embeds its font and waits for it to decode", async () => {
    const result = await run(["--dist", held]);
    assert.equal(result.code, 0, result.stderr);
    assert.match(result.stdout, /1 page\(s\) paint .* in firefox and chromium/);
  });

  // Firefox decodes an embedded font before its first paint; Chromium paints
  // one frame first.
  it("passes an embedded font in Firefox and fails it in Chromium", async () => {
    const result = await run(["--dist", embedded]);
    assert.equal(result.code, 1);
    assert.doesNotMatch(result.stderr, /^firefox /m);
    assert.match(result.stderr, /^chromium \/: font "Fixture normal normal" was not loaded at first paint/m);
  });

  it("fails a page whose font is a separate request, in both browsers", async () => {
    const dir = await site(root, "linked-font", page(`<link rel="stylesheet" href="/site.css">`), {
      "site.css": fontFace("/font.woff2"),
    });
    const result = await run(["--dist", dir]);
    assert.equal(result.code, 1);
    assert.match(result.stderr, /^firefox \/: font "Fixture normal normal" was not loaded at first paint/m);
    assert.match(result.stderr, /^chromium \/: font "Fixture normal normal" was not loaded at first paint/m);
  });

  it("fails a page whose stylesheet does not block rendering", async () => {
    const head = `<link rel="stylesheet" href="/site.css" media="print" onload="this.media='all'">`;
    const dir = await site(root, "async-sheet", page(head), { "site.css": "p { color: red; }" });
    const result = await run(["--dist", dir, "--browser", "firefox"]);
    assert.equal(result.code, 1);
    assert.match(result.stderr, /^firefox \/: stylesheet \/site\.css finished at \d+ ms, after first paint/m);
  });

  it("fails a page whose stylesheet is missing", async () => {
    const dir = await site(root, "missing-sheet", page(`<link rel="stylesheet" href="/gone.css">`));
    const result = await run(["--dist", dir, "--browser", "chromium"]);
    assert.equal(result.code, 1);
    assert.match(result.stderr, /^chromium \/: stylesheet \/gone\.css returned 404$/m);
  });

  it("checks pages a running server serves", async () => {
    const html = page(`<style>${fontFace("/font.woff2")}</style>`);
    const font = await readFile(FONT);
    const server = createServer((req, res) => {
      if (req.url === "/font.woff2") res.writeHead(200, { "content-type": "font/woff2" }).end(font);
      else res.writeHead(200, { "content-type": "text/html" }).end(html);
    });
    await new Promise((done) => server.listen(0, "127.0.0.1", () => done(undefined)));
    const address = server.address();
    assert.ok(address !== null && typeof address === "object");
    try {
      const url = `http://127.0.0.1:${address.port}/app/`;
      const result = await run(["--url", url, "--browser", "firefox"]);
      assert.equal(result.code, 1);
      assert.match(result.stderr, new RegExp(`^firefox ${url}: font "Fixture normal normal" was not loaded`, "m"));
    } finally {
      server.close();
    }
  });

  it("rejects an unknown browser before loading anything", async () => {
    const result = await run(["--dist", embedded, "--browser", "webkit"]);
    assert.equal(result.code, 2);
    assert.match(result.stderr, /unknown browser webkit/);
  });
});
