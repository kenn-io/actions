// @ts-check
// Fails when a page can paint before its stylesheets or fonts are in place.
// Each page loads in a real browser with stylesheet and font responses held
// back, so a page that depends on a late request paints first and the check
// catches it.
import { readdir, readFile } from "node:fs/promises";
import { createServer } from "node:http";
import { extname, join, relative, resolve, sep } from "node:path";
import { parseArgs } from "node:util";
import { chromium, errors, firefox } from "playwright";

const BROWSERS = { chromium, firefox };
// Long enough that a late resource cannot win the race against first paint
// on a fast CI runner.
const DEFAULT_HOLD_MS = 600;
const CONCURRENCY = 6;
// The performance mark hold-first-paint.js records when it reveals the page.
const REVEAL_MARK = "hold-first-paint";

/** @type {Record<string, string>} */
const CONTENT_TYPES = {
  ".html": "text/html; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".mjs": "text/javascript; charset=utf-8",
  ".json": "application/json",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".jpg": "image/jpeg",
  ".jpeg": "image/jpeg",
  ".gif": "image/gif",
  ".webp": "image/webp",
  ".avif": "image/avif",
  ".ico": "image/x-icon",
  ".woff": "font/woff",
  ".woff2": "font/woff2",
  ".ttf": "font/ttf",
  ".otf": "font/otf",
  ".xml": "application/xml",
  ".txt": "text/plain; charset=utf-8",
  ".wasm": "application/wasm",
};

/**
 * @typedef {{ t: number, loaded: string[] }} Frame
 * @typedef {{
 *   failedLoads: string[],
 *   paint: number | null,
 *   lateSheets: { href: string, start: number, end: number }[],
 *   faces: { key: string, name: string, status: string }[],
 *   loadedAtPaint: string[],
 * }} PageReport
 */

/**
 * Lists every HTML page under dir as a URL path, with index.html as its
 * directory.
 * @param {string} dir
 * @returns {Promise<string[]>}
 */
async function htmlPages(dir) {
  const entries = await readdir(dir, { recursive: true, withFileTypes: true });
  return entries
    .filter((entry) => entry.isFile() && entry.name.endsWith(".html"))
    .map((entry) => "/" + relative(dir, join(entry.parentPath, entry.name)).split(sep).map(encodeURIComponent).join("/"))
    .map((path) => path.replace(/(^|\/)index\.html$/, "$1"))
    .sort();
}

/**
 * @param {string} root
 * @returns {Promise<{ origin: string, close: () => void }>}
 */
function serve(root) {
  const server = createServer((req, res) => {
    let path;
    try {
      path = decodeURIComponent(new URL(req.url ?? "/", "http://localhost").pathname);
    } catch {
      res.writeHead(400).end();
      return;
    }
    const file = resolve(root, "." + (path.endsWith("/") ? path + "index.html" : path));
    if (!file.startsWith(root + sep)) {
      res.writeHead(403).end();
      return;
    }
    readFile(file).then(
      (body) => {
        res.writeHead(200, { "content-type": CONTENT_TYPES[extname(file)] ?? "application/octet-stream" });
        res.end(body);
      },
      () => res.writeHead(404).end(),
    );
  });
  return new Promise((done) =>
    server.listen(0, "127.0.0.1", () => {
      const address = server.address();
      const port = typeof address === "object" && address !== null ? address.port : 0;
      done({ origin: `http://127.0.0.1:${port}`, close: () => server.close() });
    }),
  );
}

// Runs in the page before any of its own content.
function recordFrames() {
  // Subsets of one family differ only in their unicode range.
  /** @param {FontFace} face */
  const key = (face) => `${face.family} ${face.weight} ${face.style} ${face.stretch} ${face.unicodeRange}`;
  /** @type {Frame[]} */
  const frames = [];
  /** @type {{ __frames?: Frame[] }} */ (/** @type {unknown} */ (window)).__frames = frames;
  // The frame's start time, which animation frame callbacks receive.
  // performance.now() later in the frame can run past first paint on a busy
  // machine, which would skip the frame that painted.
  let frameStart = 0;
  // Resize observers run after the frame's animation frame callbacks and its
  // style and layout, just before it paints. By then the page's own callbacks
  // have added their fonts, and Chromium has loaded any embedded font the
  // layout uses.
  const observer = new ResizeObserver(() => {
    frames.push({
      t: frameStart,
      loaded: [...document.fonts].filter((face) => face.status === "loaded").map(key),
    });
  });
  /** @param {number} start */
  const tick = (start) => {
    frameStart = start;
    // A new observation always reports, so the observer runs every frame.
    observer.unobserve(document.documentElement);
    observer.observe(document.documentElement);
    // Stop with the first frame after first paint has been recorded.
    if (performance.getEntriesByName("first-contentful-paint").length === 0) requestAnimationFrame(tick);
  };
  requestAnimationFrame(tick);
}

/**
 * Runs in the page after it loads.
 * @param {string} revealMark
 */
function readPaint(revealMark) {
  /** @param {FontFace} face */
  const key = (face) => `${face.family} ${face.weight} ${face.style} ${face.stretch} ${face.unicodeRange}`;
  const frames = /** @type {{ __frames: Frame[] }} */ (/** @type {unknown} */ (window)).__frames;
  const entry = /** @type {(PerformanceEntry & { paintTime?: number }) | undefined} */ (
    performance.getEntriesByName("first-contentful-paint")[0]
  );
  const reveal = performance.getEntriesByName(revealMark)[0];
  /** @type {number | null} */
  let paint = null;
  /** @type {Frame | undefined} */
  let atPaint;
  if (entry) {
    // Chromium reports startTime when the frame reached the screen and
    // paintTime when it painted. Fonts and styles must be ready at paint.
    const time = entry.paintTime ?? entry.startTime;
    paint = time;
    // Firefox rounds first paint down to a whole millisecond, so the frame
    // that painted can start up to 1 ms after it. Frames are 8 ms or more
    // apart, so 2 ms cannot reach the next one.
    atPaint = frames.filter((frame) => frame.t <= time + 2).at(-1);
  } else if (reveal) {
    // Firefox records no first contentful paint for some pages the hold
    // script hid. Such a page first shows in the frame after the reveal.
    paint = reveal.startTime;
    atPaint = frames.find((frame) => frame.t > reveal.startTime);
  }
  const resources = /** @type {PerformanceResourceTiming[]} */ (performance.getEntriesByType("resource"));
  // A stylesheet for other media, such as print, does not style the screen.
  // One requested after first paint, such as a route's stylesheet loaded on
  // demand, was not part of it.
  const lateSheets = [...document.styleSheets]
    .filter((sheet) => !sheet.disabled && (sheet.media.length === 0 || matchMedia(sheet.media.mediaText).matches))
    .flatMap((sheet) => (sheet.href ? [sheet.href] : []))
    .map((href) => {
      const timing = resources.find((resource) => resource.name === href);
      return { href, start: timing?.startTime ?? 0, end: timing?.responseEnd ?? Infinity };
    })
    .filter((sheet) => paint === null || (sheet.start < paint && sheet.end > paint));
  return {
    paint,
    lateSheets,
    faces: [...document.fonts].map((face) => {
      const ranges = face.unicodeRange.split(",").map((range) => range.trim());
      const subset = face.unicodeRange === "U+0-10FFFF" ? "" : ` (${ranges[0]}${ranges.length > 1 ? ", ..." : ""})`;
      const stretch = face.stretch === "normal" ? "" : ` ${face.stretch}`;
      return { key: key(face), name: `"${face.family} ${face.weight} ${face.style}${stretch}"${subset}`, status: face.status };
    }),
    loadedAtPaint: atPaint?.loaded ?? [],
  };
}

/**
 * @param {import("playwright").Browser} browser
 * @param {string} url
 * @param {number} holdMs
 * @returns {Promise<PageReport>}
 */
async function inspect(browser, url, holdMs) {
  const context = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  // A stylesheet or font that failed never appears in document.styleSheets
  // or document.fonts, so the paint checks cannot see it.
  /** @type {string[]} */
  const failedLoads = [];
  /** @param {string} type */
  const watched = (type) => type === "stylesheet" || type === "font";
  /** @type {Set<import("playwright").Request>} */
  const held = new Set();
  context.on("response", (response) => {
    const request = response.request();
    if (watched(request.resourceType()) && !response.ok()) {
      failedLoads.push(`${request.resourceType()} ${new URL(request.url()).pathname} returned ${response.status()}`);
    }
  });
  context.on("requestfinished", (request) => held.delete(request));
  context.on("requestfailed", (request) => {
    held.delete(request);
    if (watched(request.resourceType())) {
      failedLoads.push(
        `${request.resourceType()} ${new URL(request.url()).pathname} failed: ${request.failure()?.errorText ?? "unknown error"}`,
      );
    }
  });
  try {
    await context.route("**/*", async (route) => {
      if (watched(route.request().resourceType())) {
        held.add(route.request());
        await new Promise((done) => setTimeout(done, holdMs));
      }
      await route.continue();
    });
    await context.addInitScript(recordFrames);
    const page = await context.newPage();
    const response = await page.goto(url, { waitUntil: "load" });
    if (!response?.ok()) {
      throw new Error(`${url} returned ${response?.status() ?? "no response"}`);
    }
    // Wait for first paint. When the hold script revealed the page but no
    // first contentful paint follows, the report falls back to the reveal.
    await page
      .waitForFunction(
        (mark) =>
          performance.getEntriesByName("first-contentful-paint").length > 0 ||
          performance.getEntriesByName(mark).some((reveal) => performance.now() > reveal.startTime + 500),
        REVEAL_MARK,
        { timeout: 5000 },
      )
      .catch((error) => {
        // The report then records no paint, and the page fails.
        if (!(error instanceof errors.TimeoutError)) throw error;
      });
    // A stylesheet or font the page requests late, such as from a load
    // handler, is missing from the page until its held response arrives.
    for (const deadline = Date.now() + 5000; Date.now() < deadline; ) {
      await new Promise((done) => setTimeout(done, 100));
      if (held.size === 0) break;
    }
    await page.evaluate(() => document.fonts.ready);
    return { failedLoads, ...(await page.evaluate(readPaint, REVEAL_MARK)) };
  } finally {
    await context.close();
  }
}

/**
 * @param {PageReport} report
 * @returns {string[]}
 */
function problems(report) {
  if (report.paint === null) {
    return [...report.failedLoads, "no first contentful paint recorded"];
  }
  const paint = Math.round(report.paint);
  const late = report.lateSheets.map(
    (sheet) =>
      `stylesheet ${new URL(sheet.href).pathname} finished at ${Math.round(sheet.end)} ms, after first paint at ${paint} ms`,
  );
  // A face can fail to decode without any failed request.
  const broken = report.faces.filter((face) => face.status === "error").map((face) => `font ${face.name} failed to load`);
  // Faces that never left "unloaded" were not needed by the page.
  const lateFonts = report.faces
    .filter((face) => face.status === "loading" || face.status === "loaded")
    .filter((face) => !report.loadedAtPaint.includes(face.key))
    .map((face) => `font ${face.name} was not loaded at first paint (${paint} ms), so text painted in a fallback face`);
  return [...report.failedLoads, ...late, ...broken, ...lateFonts];
}

/**
 * @param {import("playwright").Browser} browser
 * @param {string[]} urls
 * @param {number} holdMs
 * @returns {Promise<{ url: string, problem: string }[]>}
 */
async function checkAll(browser, urls, holdMs) {
  /** @type {{ url: string, problem: string }[]} */
  const failures = [];
  const queue = [...urls];
  await Promise.all(
    Array.from({ length: CONCURRENCY }, async () => {
      for (let url = queue.shift(); url !== undefined; url = queue.shift()) {
        let found;
        try {
          found = problems(await inspect(browser, url, holdMs));
        } catch (error) {
          found = [error instanceof Error ? error.message : String(error)];
        }
        failures.push(...found.map((problem) => ({ url, problem })));
      }
    }),
  );
  return failures;
}

/** @param {string} message */
function usage(message) {
  console.error(`first-paint: ${message}`);
  console.error("usage: first-paint [--dist DIR] [--url URL]... [--browser firefox|chromium]... [--hold-ms N]");
  process.exit(2);
}

async function main() {
  const { values } = parseArgs({
    options: {
      dist: { type: "string" },
      url: { type: "string", multiple: true, default: [] },
      browser: { type: "string", multiple: true, default: [] },
      "hold-ms": { type: "string", default: String(DEFAULT_HOLD_MS) },
    },
  });
  const holdMs = Number(values["hold-ms"]);
  if (!Number.isInteger(holdMs) || holdMs <= 0) usage(`--hold-ms must be a positive integer, got ${values["hold-ms"]}`);
  const names = values.browser.length > 0 ? values.browser : ["firefox", "chromium"];
  const unknown = names.filter((name) => !(name in BROWSERS));
  if (unknown.length > 0) usage(`unknown browser ${unknown.join(", ")}; use firefox or chromium`);
  if (values.dist === undefined && values.url.length === 0) usage("pass --dist, --url, or both");

  const urls = [...values.url];
  /** @type {{ origin: string, close: () => void } | undefined} */
  let server;
  if (values.dist !== undefined) {
    const root = resolve(values.dist);
    const pages = await htmlPages(root);
    if (pages.length === 0) usage(`no HTML pages under ${root}; build the site first`);
    server = await serve(root);
    const { origin } = server;
    urls.push(...pages.map((path) => origin + path));
  }

  /** @type {string[]} */
  const failures = [];
  try {
    for (const name of new Set(names)) {
      const browser = await BROWSERS[/** @type {keyof typeof BROWSERS} */ (name)].launch();
      try {
        const found = await checkAll(browser, urls, holdMs);
        const label = (/** @type {string} */ url) =>
          server && url.startsWith(server.origin) ? url.slice(server.origin.length) : url;
        failures.push(...found.map(({ url, problem }) => `${name} ${label(url)}: ${problem}`));
      } finally {
        await browser.close();
      }
    }
  } finally {
    server?.close();
  }
  if (failures.length > 0) {
    console.error(failures.sort().join("\n"));
    console.error(`\n${failures.length} first-paint problem(s) across ${urls.length} page(s).`);
    process.exit(1);
  }
  console.log(`${urls.length} page(s) paint with their stylesheets and fonts in place in ${[...new Set(names)].join(" and ")}.`);
}

await main();
