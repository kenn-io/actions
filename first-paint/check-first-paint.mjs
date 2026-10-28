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
 * @typedef {{ name: string, text: string }} Waiting
 * @typedef {{ t: number, waiting: Waiting[] }} Frame
 * @typedef {{
 *   failedLoads: string[],
 *   paint: number | null,
 *   lateSheets: { href: string, start: number, end: number }[],
 *   untimedSheets: string[],
 *   faces: { name: string, status: string }[],
 *   waitingAtPaint: Waiting[] | null,
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
  // A page with many requests would otherwise drop resource timing entries
  // past the browser's default of 250.
  performance.setResourceTimingBufferSize(100000);
  // The font shorthand that document.fonts.check() takes accepts font-stretch
  // only as a keyword, and Chromium computes it as a percentage.
  /** @type {Record<string, string>} */
  const STRETCH = {
    "50%": "ultra-condensed",
    "62.5%": "extra-condensed",
    "75%": "condensed",
    "87.5%": "semi-condensed",
    "100%": "normal",
    "112.5%": "semi-expanded",
    "125%": "expanded",
    "150%": "extra-expanded",
    "200%": "ultra-expanded",
  };
  const KEYWORDS = new Set(Object.values(STRETCH));
  // Input types that draw their value or placeholder as text.
  const TEXT_INPUTS = new Set(["text", "search", "email", "url", "tel", "number", "submit", "button", "reset"]);
  /** @type {Frame[]} */
  const frames = [];
  /** @type {{ __frames?: Frame[] }} */ (/** @type {unknown} */ (window)).__frames = frames;
  /** @param {string} name */
  const unquote = (name) => name.trim().replace(/^["']|["']$/g, "");
  /** @param {DOMRectList} rects */
  const onScreen = (rects) =>
    [...rects].some(
      (rect) =>
        rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.bottom > 0 && rect.left < innerWidth && rect.top < innerHeight,
    );
  // An element with display: contents has no box, so opacity and display
  // are judged on the nearest ancestor that has one.
  /** @param {Element} element */
  const withBox = (element) => {
    let current = element;
    while (current.parentElement && getComputedStyle(current).display === "contents") current = current.parentElement;
    return current;
  };
  // The strings in a computed content value, such as an icon font's glyph.
  /** @param {string} content */
  const contentText = (content) =>
    [...content.matchAll(/"((?:[^"\\]|\\.)*)"/g)]
      .map((match) =>
        (match[1] ?? "")
          .replace(/\\([0-9a-fA-F]{1,6}) ?/g, (_, hex) => String.fromCodePoint(parseInt(hex, 16)))
          .replace(/\\(.)/g, "$1"),
      )
      .join("");
  // Lists the fonts that text on screen asks for and the browser has not
  // loaded for that text, so the frame draws it in a fallback face, or not
  // at all during a block period.
  const waiting = () => {
    if (!document.body || !document.documentElement.checkVisibility({ opacityProperty: true })) return [];
    const declared = new Set([...document.fonts].map((face) => unquote(face.family)));
    /** @type {Map<string, { font: string, name: string, chars: Set<string>, sample: string }>} */
    const fonts = new Map();
    /**
     * @param {CSSStyleDeclaration} style
     * @param {Element} element
     * @param {string} raw
     */
    const add = (style, element, raw) => {
      if (style.visibility !== "visible" || !raw.trim()) return;
      // The browser uses the first family in the list that has a face, so a
      // later family is only a fallback. A list without one uses system fonts.
      const family = style.fontFamily.split(",").map(unquote).find((name) => declared.has(name));
      if (family === undefined) return;
      const lang = element.closest("[lang]")?.getAttribute("lang") || undefined;
      // text-transform changes the characters drawn, and with them the
      // unicode-range subset they need.
      const text =
        style.textTransform === "uppercase"
          ? raw.toLocaleUpperCase(lang)
          : style.textTransform === "lowercase"
            ? raw.toLocaleLowerCase(lang)
            : style.textTransform === "capitalize"
              ? raw.replace(/(^|\s)(\S)/gu, (_, space, letter) => space + letter.toLocaleUpperCase(lang))
              : raw;
      const stretch = STRETCH[style.fontStretch] ?? style.fontStretch;
      const keyword = KEYWORDS.has(stretch) ? stretch : "normal";
      const font = `${style.fontStyle} ${style.fontWeight} ${keyword} ${style.fontSize} ${JSON.stringify(family)}`;
      const name = `${family} ${style.fontWeight} ${style.fontStyle}${keyword === "normal" ? "" : ` ${keyword}`}`;
      const entry = fonts.get(font) ?? { font, name, chars: new Set(), sample: text.trim().slice(0, 40) };
      for (const char of text) if (char.trim()) entry.chars.add(char);
      fonts.set(font, entry);
    };
    /** @param {Element} element */
    const visit = (element) => {
      if (!element.checkVisibility({ opacityProperty: true }) || !onScreen(element.getClientRects())) return;
      for (const pseudo of ["::before", "::after"]) {
        const style = getComputedStyle(element, pseudo);
        add(style, element, contentText(style.content));
      }
      const field =
        element instanceof HTMLTextAreaElement || (element instanceof HTMLInputElement && TEXT_INPUTS.has(element.type));
      if (!field) return;
      if (element.value) add(getComputedStyle(element), element, element.value);
      else add(getComputedStyle(element, "::placeholder"), element, element.placeholder);
    };
    /** @param {Node} root */
    const walk = (root) => {
      const walker = document.createTreeWalker(root, NodeFilter.SHOW_ELEMENT | NodeFilter.SHOW_TEXT, {
        acceptNode: (node) =>
          node instanceof Element && getComputedStyle(node).display === "none"
            ? NodeFilter.FILTER_REJECT
            : NodeFilter.FILTER_ACCEPT,
      });
      for (let node = walker.nextNode(); node; node = walker.nextNode()) {
        if (node instanceof Element) {
          visit(node);
          // Open shadow roots render their own text, as web components do.
          if (node.shadowRoot) walk(node.shadowRoot);
        } else if (node instanceof Text && node.parentElement && node.data.trim()) {
          const parent = node.parentElement;
          if (!withBox(parent).checkVisibility({ opacityProperty: true })) continue;
          const range = document.createRange();
          range.selectNodeContents(node);
          if (onScreen(range.getClientRects())) add(getComputedStyle(parent), parent, node.data);
        }
      }
    };
    walk(document.body);
    return [...fonts.values()]
      .filter((entry) => !document.fonts.check(entry.font, [...entry.chars].join("")))
      .map((entry) => ({ name: entry.name, text: entry.sample }));
  };
  // Resize observers run after the frame's animation frame callbacks and its
  // style and layout, just before it paints. By then the page's own callbacks
  // have added their text and fonts. Both browsers time first paint at the
  // moment the painting frame's observers ran.
  const observer = new ResizeObserver(() => frames.push({ t: performance.now(), waiting: waiting() }));
  const tick = () => {
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
    // The frame that painted is the last one sampled by then. Firefox rounds
    // both times to a millisecond, and the next sample comes 6 ms or more
    // later, so 1 ms of slack cannot reach it. A frame can start before the
    // previous one paints on a slow machine, so the frame's start time
    // cannot tell them apart.
    atPaint = frames.filter((frame) => frame.t <= time + 1).at(-1);
  } else if (reveal) {
    // Firefox records no first contentful paint for some pages the hold
    // script hid. Such a page first shows in the frame sampled after the
    // reveal.
    paint = reveal.startTime;
    atPaint = frames.find((frame) => frame.t > reveal.startTime);
  }
  const resources = /** @type {PerformanceResourceTiming[]} */ (performance.getEntriesByType("resource"));
  // A stylesheet for other media, such as print, does not style the screen.
  // One requested after first paint, such as a route's stylesheet loaded on
  // demand, was not part of it.
  /** @param {CSSStyleSheet} sheet */
  const applies = (sheet) => !sheet.disabled && (sheet.media.length === 0 || matchMedia(sheet.media.mediaText).matches);
  /**
   * The sheet and every sheet its import rules pull in.
   * @param {CSSStyleSheet} sheet
   * @returns {CSSStyleSheet[]}
   */
  const withImports = (sheet) => {
    let rules;
    try {
      rules = [...sheet.cssRules];
    } catch (error) {
      // A stylesheet from another origin hides its rules, imports included.
      if (error instanceof DOMException && error.name === "SecurityError") return [sheet];
      throw error;
    }
    return [
      sheet,
      ...rules.flatMap((rule) => (rule instanceof CSSImportRule && rule.styleSheet && applies(rule.styleSheet) ? withImports(rule.styleSheet) : [])),
    ];
  };
  // An imported sheet for other media is skipped with everything it imports.
  const sheets = [...document.styleSheets]
    .filter(applies)
    .flatMap(withImports)
    .flatMap((sheet) => (sheet.href ? [sheet.href] : []))
    .map((href) => ({ href, timing: resources.find((resource) => resource.name === href) }));
  const lateSheets = sheets
    .flatMap(({ href, timing }) => (timing ? [{ href, start: timing.startTime, end: timing.responseEnd }] : []))
    .filter((sheet) => paint === null || (sheet.start < paint && sheet.end > paint));
  const untimedSheets = sheets.flatMap(({ href, timing }) => (timing ? [] : [href]));
  return {
    paint,
    lateSheets,
    untimedSheets,
    faces: [...document.fonts].map((face) => {
      const ranges = face.unicodeRange.split(",").map((range) => range.trim());
      const subset = face.unicodeRange === "U+0-10FFFF" ? "" : ` (${ranges[0]}${ranges.length > 1 ? ", ..." : ""})`;
      const stretch = face.stretch === "normal" ? "" : ` ${face.stretch}`;
      return { name: `"${face.family} ${face.weight} ${face.style}${stretch}"${subset}`, status: face.status };
    }),
    waitingAtPaint: atPaint?.waiting ?? null,
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
    // A redirect is judged by the response it leads to.
    const redirect = response.status() >= 300 && response.status() < 400;
    if (watched(request.resourceType()) && !response.ok() && !redirect) {
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
    // A request still open here may be one the first paint waited on.
    const stalled = [...held];
    failedLoads.push(
      ...stalled.map(
        (request) => `${request.resourceType()} ${new URL(request.url()).pathname} was still loading 5 s after first paint`,
      ),
    );
    const fontsReady = await page.evaluate(() =>
      Promise.race([
        document.fonts.ready.then(() => true),
        new Promise((done) => setTimeout(() => done(false), 5000)),
      ]),
    );
    if (!fontsReady && !stalled.some((request) => request.resourceType() === "font")) {
      failedLoads.push("fonts were still loading 5 s after first paint");
    }
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
  const untimed = report.untimedSheets.map(
    (href) => `stylesheet ${new URL(href).pathname} has no resource timing entry, so its finish time is unknown`,
  );
  // A face can fail to decode without any failed request.
  const broken = report.faces.filter((face) => face.status === "error").map((face) => `font ${face.name} failed to load`);
  if (report.waitingAtPaint === null) {
    return [...report.failedLoads, ...late, ...untimed, ...broken, "no frame recorded at first paint"];
  }
  // Text that asked for a font the browser had not loaded painted in a
  // fallback face.
  const lateFonts = [...new Map(report.waitingAtPaint.map((entry) => [entry.name, entry])).values()].map(
    (entry) =>
      `font "${entry.name}" was not loaded at first paint (${paint} ms), so ${JSON.stringify(entry.text)} painted in a fallback face or not at all`,
  );
  return [...report.failedLoads, ...late, ...untimed, ...broken, ...lateFonts];
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
