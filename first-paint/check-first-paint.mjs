// @ts-check
// Fails when a page can paint before its stylesheets or fonts are in place.
// Each page loads in a real browser with stylesheet and font responses held
// back, so a page that depends on a late request paints first and the check
// catches it.
import { readdir, readFile } from "node:fs/promises";
import { createServer } from "node:http";
import { extname, join, relative, resolve, sep } from "node:path";
import { parseArgs } from "node:util";
import { chromium, firefox } from "playwright";

const BROWSERS = { chromium, firefox };
// Long enough that a late resource cannot win the race against first paint
// on a fast CI runner.
const DEFAULT_HOLD_MS = 600;
const CONCURRENCY = 6;

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
 *   fcp: number | null,
 *   lateSheets: { href: string, end: number }[],
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
    .map((entry) => "/" + relative(dir, join(entry.parentPath, entry.name)).split(sep).join("/"))
    .map((path) => path.replace(/(^|\/)index\.html$/, "$1"))
    .sort();
}

/**
 * @param {string} root
 * @returns {Promise<{ origin: string, close: () => void }>}
 */
function serve(root) {
  const server = createServer((req, res) => {
    const path = decodeURIComponent(new URL(req.url ?? "/", "http://localhost").pathname);
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
  const key = (face) => `${face.family} ${face.weight} ${face.style} ${face.unicodeRange}`;
  /** @type {Frame[]} */
  const frames = [];
  /** @type {{ __frames?: Frame[] }} */ (/** @type {unknown} */ (window)).__frames = frames;
  // Record the frame's start time, which the callback receives.
  // performance.now() inside the callback can run past first paint on a busy
  // machine, which would skip the frame that painted.
  /** @param {number} frameStart */
  const sample = (frameStart) => {
    // Animation frame callbacks run before the frame's style and layout,
    // which is where Chromium loads an embedded font. Run them now, as the
    // frame would before it paints, so the sample sees what the paint uses.
    void document.documentElement.offsetHeight;
    frames.push({
      t: frameStart,
      loaded: [...document.fonts].filter((face) => face.status === "loaded").map(key),
    });
    if (document.readyState !== "complete" || frames.length < 30) requestAnimationFrame(sample);
  };
  requestAnimationFrame(sample);
}

// Runs in the page after it loads.
function readPaint() {
  /** @param {FontFace} face */
  const key = (face) => `${face.family} ${face.weight} ${face.style} ${face.unicodeRange}`;
  const entry = /** @type {(PerformanceEntry & { paintTime?: number }) | undefined} */ (
    performance.getEntriesByName("first-contentful-paint")[0]
  );
  // Chromium reports startTime when the frame reached the screen and
  // paintTime when it painted. Fonts and styles must be ready at paint.
  const fcp = entry ? (entry.paintTime ?? entry.startTime) : null;
  const resources = /** @type {PerformanceResourceTiming[]} */ (performance.getEntriesByType("resource"));
  const lateSheets = [...document.styleSheets]
    .flatMap((sheet) => (sheet.href ? [sheet.href] : []))
    .map((href) => ({ href, end: resources.find((resource) => resource.name === href)?.responseEnd ?? Infinity }))
    .filter((sheet) => fcp === null || sheet.end > fcp);
  const frames = /** @type {{ __frames: Frame[] }} */ (/** @type {unknown} */ (window)).__frames;
  // Firefox rounds first paint down to a whole millisecond, so the frame
  // that painted can start up to 1 ms after it. Frames are 8 ms or more
  // apart, so 2 ms cannot reach the next one.
  const beforePaint = frames.filter((frame) => fcp !== null && frame.t <= fcp + 2);
  return {
    fcp,
    lateSheets,
    faces: [...document.fonts].map((face) => {
      const ranges = face.unicodeRange.split(",").map((range) => range.trim());
      const subset = face.unicodeRange === "U+0-10FFFF" ? "" : ` (${ranges[0]}${ranges.length > 1 ? ", ..." : ""})`;
      return { key: key(face), name: `"${face.family} ${face.weight} ${face.style}"${subset}`, status: face.status };
    }),
    loadedAtPaint: beforePaint.at(-1)?.loaded ?? [],
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
  context.on("response", (response) => {
    const request = response.request();
    if (watched(request.resourceType()) && !response.ok()) {
      failedLoads.push(`${request.resourceType()} ${new URL(request.url()).pathname} returned ${response.status()}`);
    }
  });
  context.on("requestfailed", (request) => {
    if (watched(request.resourceType())) {
      failedLoads.push(
        `${request.resourceType()} ${new URL(request.url()).pathname} failed: ${request.failure()?.errorText ?? "unknown error"}`,
      );
    }
  });
  try {
    await context.route("**/*", async (route) => {
      if (watched(route.request().resourceType())) {
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
    await page.evaluate(() => document.fonts.ready);
    await page.waitForFunction(() => performance.getEntriesByName("first-contentful-paint").length > 0, null, {
      timeout: 5000,
    });
    return { failedLoads, ...(await page.evaluate(readPaint)) };
  } finally {
    await context.close();
  }
}

/**
 * @param {PageReport} report
 * @returns {string[]}
 */
function problems(report) {
  if (report.fcp === null) {
    return [...report.failedLoads, "no first contentful paint recorded"];
  }
  const fcp = Math.round(report.fcp);
  const late = report.lateSheets.map(
    (sheet) =>
      `stylesheet ${new URL(sheet.href).pathname} finished at ${Math.round(sheet.end)} ms, after first paint at ${fcp} ms`,
  );
  // A face can fail to decode without any failed request.
  const broken = report.faces.filter((face) => face.status === "error").map((face) => `font ${face.name} failed to load`);
  // Faces that never left "unloaded" were not needed by the page.
  const lateFonts = report.faces
    .filter((face) => face.status === "loading" || face.status === "loaded")
    .filter((face) => !report.loadedAtPaint.includes(face.key))
    .map((face) => `font ${face.name} was not loaded at first paint (${fcp} ms), so text painted in a fallback face`);
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
