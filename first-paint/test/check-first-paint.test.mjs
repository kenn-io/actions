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

/**
 * Serves handler on a local port while check runs against its origin.
 * @param {import("node:http").RequestListener} handler
 * @param {(origin: string) => Promise<void>} check
 */
async function withServer(handler, check) {
  const server = createServer(handler);
  await new Promise((done) => server.listen(0, "127.0.0.1", () => done(undefined)));
  const address = server.address();
  assert.ok(address !== null && typeof address === "object");
  try {
    await check(`http://127.0.0.1:${address.port}`);
  } finally {
    server.closeAllConnections();
    server.close();
  }
}

describe("first-paint", () => {
  /** @type {string} */
  let root;
  /** @type {string} */
  let embedded;
  /** @type {string} */
  let held;
  /** @type {string} */
  let data;
  /** @type {string} */
  let hold;

  before(async () => {
    root = await mkdtemp(join(tmpdir(), "first-paint-"));
    data = `data:font/woff2;base64,${(await readFile(FONT)).toString("base64")}`;
    embedded = await site(root, "embedded", page(`<link rel="stylesheet" href="/site.css">`), {
      "site.css": fontFace(data),
    });
    // Embeds its font and inlines the hold script, as the README tells a UI to do.
    hold = await readFile(HOLD, "utf8");
    held = await site(root, "held", page(`<script>${hold}</script><link rel="stylesheet" href="/site.css">`), {
      "site.css": fontFace(data),
    });
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
    assert.match(result.stderr, /^chromium \/: font "Fixture 400 normal" was not loaded at first paint/m);
  });

  it("fails a page whose font is a separate request, in both browsers", async () => {
    const dir = await site(root, "linked-font", page(`<link rel="stylesheet" href="/site.css">`), {
      "site.css": fontFace("/font.woff2"),
    });
    const result = await run(["--dist", dir]);
    assert.equal(result.code, 1);
    assert.match(result.stderr, /^firefox \/: font "Fixture 400 normal" was not loaded at first paint/m);
    assert.match(result.stderr, /^chromium \/: font "Fixture 400 normal" was not loaded at first paint/m);
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
    const handler = (/** @type {import("node:http").IncomingMessage} */ req, /** @type {import("node:http").ServerResponse} */ res) => {
      if (req.url === "/font.woff2") res.writeHead(200, { "content-type": "font/woff2" }).end(font);
      else res.writeHead(200, { "content-type": "text/html" }).end(html);
    };
    await withServer(handler, async (origin) => {
      const url = `${origin}/app/`;
      const result = await run(["--url", url, "--browser", "firefox"]);
      assert.equal(result.code, 1);
      assert.match(result.stderr, new RegExp(`^firefox ${url}: font "Fixture 400 normal" was not loaded`, "m"));
    });
  });

  it("follows a redirected stylesheet", async () => {
    const html = page(`<link rel="stylesheet" href="/old.css">`);
    const handler = (/** @type {import("node:http").IncomingMessage} */ req, /** @type {import("node:http").ServerResponse} */ res) => {
      if (req.url === "/old.css") res.writeHead(301, { location: "/new.css" }).end();
      else if (req.url === "/new.css") res.writeHead(200, { "content-type": "text/css" }).end("p { color: black; }");
      else res.writeHead(200, { "content-type": "text/html" }).end(html);
    };
    await withServer(handler, async (origin) => {
      const result = await run(["--url", `${origin}/`, "--browser", "firefox"]);
      assert.equal(result.code, 0, result.stderr);
    });
  });

  it("reports a font that never finishes loading", { timeout: 60000 }, async () => {
    const script = `addEventListener("load", () => setTimeout(() => document.fonts.load("16px Fixture"), 50))`;
    const html = `<!doctype html><html><head><meta charset="utf-8"><style>${fontFace("/stall.woff2")}</style><script>${script}</script></head><body><p>plain</p></body></html>`;
    const handler = (/** @type {import("node:http").IncomingMessage} */ req, /** @type {import("node:http").ServerResponse} */ res) => {
      // The font request never gets a response.
      if (req.url !== "/stall.woff2") res.writeHead(200, { "content-type": "text/html" }).end(html);
    };
    await withServer(handler, async (origin) => {
      const result = await run(["--url", `${origin}/`, "--browser", "firefox"]);
      assert.equal(result.code, 1);
      assert.match(result.stderr, /: fonts were still loading 5 s after first paint$/m);
    });
  });

  it("hides text that sets its own visibility until its font decodes", async () => {
    const text = `<p style="font-family: Fixture, monospace; visibility: visible">The quick brown fox</p>`;
    const html = `<!doctype html><html><head><meta charset="utf-8"><script>${hold}</script><link rel="stylesheet" href="/site.css"></head><body>${text}</body></html>`;
    const dir = await site(root, "visible-text", html, { "site.css": fontFace(data) });
    const result = await run(["--dist", dir, "--browser", "chromium"]);
    assert.equal(result.code, 0, result.stderr);
  });

  // Firefox records no first contentful paint for this page, so the check
  // uses the time the hold script revealed it.
  it("passes a held page that scrolls before it is shown", async () => {
    const items = Array.from({ length: 9 }, (_, i) => `<p id="p${i}">${i}</p>`).join("");
    const body = `<div style="height: 100px; overflow: auto; font-family: Fixture">${items}</div><script>document.getElementById("p8").scrollIntoView()</script>`;
    const html = `<!doctype html><html><head><meta charset="utf-8"><script>${hold}</script><link rel="stylesheet" href="/site.css"></head><body>${body}</body></html>`;
    const dir = await site(root, "scrolled", html, { "site.css": fontFace(data) });
    const result = await run(["--dist", dir, "--browser", "firefox"]);
    assert.equal(result.code, 0, result.stderr);
  });

  it("checks a face of one stretch apart from the same face in another", async () => {
    const css = `${fontFace(data)} @font-face { font-family: Fixture; font-stretch: condensed; src: url("/font.woff2") format("woff2"); }`;
    const html = `<!doctype html><html><head><meta charset="utf-8"><link rel="stylesheet" href="/site.css"></head><body><p style="font-family: Fixture">Normal</p><p style="font-family: Fixture; font-stretch: condensed">Condensed</p></body></html>`;
    const dir = await site(root, "stretch", html, { "site.css": css });
    const result = await run(["--dist", dir, "--browser", "firefox"]);
    assert.equal(result.code, 1);
    assert.match(result.stderr, /^firefox \/: font "Fixture 400 normal condensed" was not loaded at first paint/m);
  });

  it("passes a page that loads its font before it first shows text", async () => {
    const script = `setTimeout(async () => { await document.fonts.load("16px Fixture"); document.body.innerHTML = ${JSON.stringify(BODY)}; }, 1500)`;
    const html = `<!doctype html><html><head><meta charset="utf-8"><link rel="stylesheet" href="/site.css"></head><body><script>${script}</script></body></html>`;
    const dir = await site(root, "late-text", html, { "site.css": fontFace("/font.woff2") });
    const result = await run(["--dist", dir, "--browser", "firefox"]);
    assert.equal(result.code, 0, result.stderr);
  });

  it("passes a page whose animation frame adds a loaded font with its text", async () => {
    const script = `const face = new FontFace("Fixture", "url(${data})"); face.load().then(() => requestAnimationFrame(() => { document.fonts.add(face); document.body.innerHTML = ${JSON.stringify(BODY)}; }));`;
    const html = `<!doctype html><html><head><meta charset="utf-8"></head><body><script>${script}</script></body></html>`;
    const dir = await site(root, "added-face", html);
    const result = await run(["--dist", dir, "--browser", "firefox"]);
    assert.equal(result.code, 0, result.stderr);
  });

  it("ignores a stylesheet for other media", async () => {
    const dir = await site(root, "print-sheet", page(`<link rel="stylesheet" href="/print.css" media="print">`), {
      "print.css": "p { color: black; }",
    });
    const result = await run(["--dist", dir, "--browser", "firefox"]);
    assert.equal(result.code, 0, result.stderr);
  });

  it("ignores a stylesheet the page requests after its first paint", async () => {
    const script = `new PerformanceObserver(() => { const link = document.createElement("link"); link.rel = "stylesheet"; link.href = "/route.css"; document.head.append(link); }).observe({ type: "paint" })`;
    const dir = await site(root, "route-sheet", page(`<script>${script}</script>`), { "route.css": "p { color: black; }" });
    const result = await run(["--dist", dir, "--browser", "firefox"]);
    assert.equal(result.code, 0, result.stderr);
  });

  it("checks a page whose file name needs URL encoding", async () => {
    const dir = await site(root, "encoded", page(""), { "a#b.html": page("") });
    const result = await run(["--dist", dir, "--browser", "firefox"]);
    assert.equal(result.code, 0, result.stderr);
    assert.match(result.stdout, /^2 page\(s\)/);
  });

  it("reports a request with a malformed path", async () => {
    const dir = await site(root, "malformed", page(`<link rel="stylesheet" href="/%E0%A4%A.css">`));
    const result = await run(["--dist", dir, "--browser", "firefox"]);
    assert.equal(result.code, 1);
    assert.match(result.stderr, /^firefox \/: stylesheet \/%E0%A4%A\.css returned 400$/m);
  });

  it("checks a stylesheet a script imports before first paint", async () => {
    const script = `const style = document.createElement("style"); style.textContent = "@import url(/late.css);"; document.head.append(style);`;
    const dir = await site(root, "imported", page(`<script>${script}</script>`), { "late.css": "p { color: black; }" });
    const result = await run(["--dist", dir, "--browser", "firefox"]);
    assert.equal(result.code, 1);
    assert.match(result.stderr, /^firefox \/: stylesheet \/late\.css finished at \d+ ms, after first paint/m);
  });

  it("passes a page that shows text in a font it loads after first paint", async () => {
    const script = `setTimeout(async () => { await document.fonts.load("16px Fixture"); document.body.insertAdjacentHTML("beforeend", ${JSON.stringify(BODY)}); }, 30)`;
    const html = `<!doctype html><html><head><meta charset="utf-8"><link rel="stylesheet" href="/site.css"></head><body><h1>Heading</h1><script>${script}</script></body></html>`;
    const dir = await site(root, "font-after-paint", html, { "site.css": fontFace("/font.woff2") });
    const result = await run(["--dist", dir]);
    assert.equal(result.code, 0, result.stderr);
  });

  it("passes a page that starts loading its font before first paint and shows its text after", async () => {
    const script = `document.fonts.load("16px Fixture").then(() => document.body.insertAdjacentHTML("beforeend", ${JSON.stringify(BODY)}))`;
    const html = `<!doctype html><html><head><meta charset="utf-8"><link rel="stylesheet" href="/site.css"><script>${script}</script></head><body><h1>Heading</h1></body></html>`;
    const dir = await site(root, "preloaded-font", html, { "site.css": fontFace("/font.woff2") });
    const result = await run(["--dist", dir]);
    assert.equal(result.code, 0, result.stderr);
  });

  it("holds the page for a stylesheet whose rel is not lower case", async () => {
    const head = `<script>${hold}</script><link rel="Stylesheet " href="/site.css">`;
    const dir = await site(root, "rel-case", page(head), { "site.css": fontFace(data) });
    const result = await run(["--dist", dir, "--browser", "chromium"]);
    assert.equal(result.code, 0, result.stderr);
  });

  it("rejects an unknown browser before loading anything", async () => {
    const result = await run(["--dist", embedded, "--browser", "webkit"]);
    assert.equal(result.code, 2);
    assert.match(result.stderr, /unknown browser webkit/);
  });
});
