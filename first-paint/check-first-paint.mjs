// @ts-check
// Fails when a page can paint before its stylesheets or fonts are in place.
// A page passes when it inlines hold-first-paint.js in <head> before any
// stylesheet, links each stylesheet with a plain <link> in <head>, and embeds
// every font in its stylesheets as a data: URL. The check reads HTML and CSS
// only, so it cannot see a stylesheet or font that a script adds at runtime.
import { readFile, readdir } from "node:fs/promises";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { parseArgs } from "node:util";
import { parse } from "parse5";
import postcss, { AtRule, Container } from "postcss";

/** @typedef {import("parse5").DefaultTreeAdapterMap["element"]} Element */
/** @typedef {import("parse5").DefaultTreeAdapterMap["node"]} Node */
/** @typedef {(url: URL) => Promise<{ text: string, url: URL } | undefined>} Load */

// Pages under --dist resolve against this origin, so relative and absolute
// stylesheet URLs map onto files the way a server would serve them.
const DIST_ORIGIN = "http://dist.invalid";

// Repositories put their own comment above the copy they inline, so only the
// code after the leading comment lines has to match.
const code = (/** @type {string} */ script) => {
  const lines = script.trim().split("\n");
  const start = lines.findIndex((line) => line.trim() !== "" && !line.trim().startsWith("//"));
  return start === -1 ? "" : lines.slice(start).join("\n").trim();
};

const HOLD = code(await readFile(new URL("hold-first-paint.js", import.meta.url), "utf8"));

// Template contents are inert, so the walk skips them.
/** @param {Node} node @returns {Generator<Element>} */
function* elements(node) {
  if ("tagName" in node) yield node;
  for (const child of "childNodes" in node ? node.childNodes : []) yield* elements(child);
}

const attr = (/** @type {Element} */ el, /** @type {string} */ name) =>
  el.attrs.find((a) => a.name === name)?.value;

const text = (/** @type {Element} */ el) =>
  el.childNodes.map((child) => ("value" in child ? child.value : "")).join("");

const rel = (/** @type {Element} */ el) => (attr(el, "rel") ?? "").toLowerCase().split(/\s+/);

const isStylesheet = (/** @type {Element} */ el) =>
  el.tagName === "link" && rel(el).includes("stylesheet") && !rel(el).includes("alternate");

// A classic script runs while the parser reaches it. A module, nomodule, or
// non-JavaScript script would not hide the page before its first paint.
const runsInline = (/** @type {Element} */ el) =>
  el.tagName === "script" &&
  attr(el, "src") === undefined &&
  attr(el, "nomodule") === undefined &&
  ["", "text/javascript", "application/javascript"].includes((attr(el, "type") ?? "").trim().toLowerCase());

// A print-only stylesheet never styles the screen, so it cannot flash. Every
// query in the list has to be for print.
const isPrint = (/** @type {string | undefined} */ media) =>
  (media ?? "").split(",").every((query) => /^\s*(?:only\s+)?print(?:\s+and\b.*)?$/is.test(query));

// A stylesheet a script swaps in after it loads, such as the media="print"
// onload trick or a preload whose onload sets rel, never blocks first paint.
// An onload handler on a stylesheet that already styles the screen, such as
// one that records a performance mark, does not change that.
const isSwapped = (/** @type {Element} */ el) => {
  if (el.tagName !== "link" || attr(el, "onload") === undefined) return false;
  const screen = ["", "all", "screen"].includes((attr(el, "media") ?? "").trim().toLowerCase());
  return (isStylesheet(el) && !screen) || (rel(el).includes("preload") && attr(el, "as")?.toLowerCase() === "style");
};

/** @param {AtRule} rule */
const inPrintMedia = (rule) => {
  /** @type {unknown} */
  let node = rule.parent;
  while (node instanceof Container) {
    if (node instanceof AtRule && node.name.toLowerCase() === "media" && isPrint(node.params)) return true;
    node = node.parent;
  }
  return false;
};

const URL_TOKEN = String.raw`url\(\s*(?:"([^"]*)"|'([^']*)'|([^)\s]*))\s*\)`;
const QUOTED = String.raw`"([^"]*)"|'([^']*)'`;
const IMPORT = new RegExp(String.raw`^\s*(?:${URL_TOKEN}|${QUOTED})(.*)$`, "is");
const firstGroup = (/** @type {RegExpMatchArray} */ match) => match.slice(1, 6).find((group) => group !== undefined) ?? "";

/**
 * Lists problems in one stylesheet and the stylesheets it imports.
 * An inline <style> that imports a stylesheet fails: the hold script waits only
 * for <link> stylesheets, so it would show the page before the import loads.
 * @param {string} css @param {URL} base @param {URL} page @param {Load} load @param {Set<string>} seen
 * @param {boolean} [inline]
 * @returns {Promise<string[]>}
 */
async function checkCss(css, base, page, load, seen, inline = false) {
  let root;
  try {
    root = postcss.parse(css);
  } catch {
    return [`stylesheet ${base.pathname} could not be parsed`];
  }
  const problems = [];
  /** @type {URL[]} */
  const imports = [];
  root.walkAtRules((rule) => {
    const name = rule.name.toLowerCase();
    if (name === "font-face" && !inPrintMedia(rule)) {
      rule.walkDecls(/^src$/i, (decl) => {
        for (const match of decl.value.matchAll(new RegExp(URL_TOKEN, "gi"))) {
          const url = firstGroup(match);
          if (!/^data:/i.test(url)) {
            problems.push(`font ${url} in ${base.pathname} loads as a separate file; embed it as a data: URL`);
          }
        }
      });
    } else if (name === "import") {
      const match = rule.params.match(IMPORT);
      if (!match || isPrint(match[6])) return;
      const url = new URL(firstGroup(match), base);
      if (inline) {
        problems.push(`an inline <style> imports ${url.pathname}; move the import into a linked stylesheet`);
      } else {
        imports.push(url);
      }
    }
  });
  for (const url of imports) problems.push(...(await checkSheet(url, page, load, seen)));
  return problems;
}

/**
 * @param {URL} url @param {URL} page @param {Load} load @param {Set<string>} seen
 * @returns {Promise<string[]>}
 */
async function checkSheet(url, page, load, seen) {
  const elsewhere = `is on another origin; serve it with the site and embed its fonts`;
  if (url.origin !== page.origin) return [`stylesheet ${url.href} ${elsewhere}`];
  if (seen.has(url.href)) return [];
  seen.add(url.href);
  const loaded = await load(url);
  if (loaded === undefined) return [`stylesheet ${url.pathname} was not found`];
  if (loaded.url.origin !== page.origin) return [`stylesheet ${url.pathname} redirects to ${loaded.url.href}, which ${elsewhere}`];
  return checkCss(loaded.text, loaded.url, page, load, seen);
}

/**
 * Lists the reasons one page can paint before its stylesheets or fonts.
 * @param {string} html @param {URL} page @param {Load} load
 * @returns {Promise<string[]>}
 */
export async function checkPage(html, page, load) {
  const problems = [];
  const all = [...elements(parse(html))];
  const head = all.find((el) => el.tagName === "head");
  const inHead = new Set(head ? elements(head) : []);
  const baseHref = all.find((el) => el.tagName === "base" && attr(el, "href") !== undefined);
  const base = baseHref ? new URL(attr(baseHref, "href") ?? "", page) : page;
  const hold = all.findIndex((el) => runsInline(el) && inHead.has(el) && code(text(el)) === HOLD);
  /** @type {Set<string>} */
  const seen = new Set();
  let first = -1;
  for (const [index, el] of all.entries()) {
    if (isSwapped(el)) {
      problems.push(`stylesheet ${attr(el, "href") ?? ""} is swapped in by an onload handler; link it with a plain <link rel="stylesheet">`);
    } else if (el.tagName === "style" && !isPrint(attr(el, "media"))) {
      if (first === -1) first = index;
      problems.push(...(await checkCss(text(el), base, page, load, seen, true)));
    } else if (isStylesheet(el) && !isPrint(attr(el, "media"))) {
      if (first === -1) first = index;
      const href = attr(el, "href") ?? "";
      if (!inHead.has(el)) problems.push(`stylesheet ${href} is outside <head>`);
      problems.push(...(await checkSheet(new URL(href, base), page, load, seen)));
    }
  }
  if (first !== -1 && hold === -1) {
    problems.push("<head> does not inline this action's hold-first-paint.js");
  } else if (first !== -1 && hold > first) {
    problems.push("hold-first-paint.js comes after the first stylesheet");
  }
  return problems;
}

/** @type {Load} */
async function fetchText(url) {
  const response = await fetch(url);
  return response.ok ? { text: await response.text(), url: new URL(response.url) } : undefined;
}

/**
 * Checks every .html file under dist and every page URL.
 * @param {{ dist?: string, urls?: string[] }} options
 * @returns {Promise<Map<string, string[]>>} problems by page, for pages that have any
 */
export async function check({ dist, urls = [] }) {
  /** @type {Map<string, string[]>} */
  const results = new Map();
  if (dist) {
    const root = path.resolve(dist);
    /** @type {Load} */
    const load = async (url) => {
      const file = path.join(root, decodeURIComponent(url.pathname));
      if (path.relative(root, file).startsWith("..")) return undefined;
      const text = await readFile(file, "utf8").catch(() => undefined);
      return text === undefined ? undefined : { text, url };
    };
    const entries = await readdir(root, { recursive: true, withFileTypes: true });
    const files = entries
      .filter((entry) => entry.isFile() && entry.name.endsWith(".html"))
      .map((entry) => path.relative(root, path.join(entry.parentPath, entry.name)).split(path.sep).join("/"))
      .sort();
    for (const file of files) {
      const html = await readFile(path.join(root, file), "utf8");
      const problems = await checkPage(html, new URL(`/${file}`, DIST_ORIGIN), load);
      if (problems.length > 0) results.set(file, problems);
    }
  }
  for (const url of urls) {
    const loaded = await fetchText(new URL(url));
    const problems = loaded === undefined ? ["page returned an error"] : await checkPage(loaded.text, loaded.url, fetchText);
    if (problems.length > 0) results.set(url, problems);
  }
  return results;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const { values } = parseArgs({
    options: { dist: { type: "string" }, url: { type: "string", multiple: true } },
  });
  if (!values.dist && !values.url) {
    console.error("usage: check-first-paint.mjs [--dist <dir>] [--url <url>]...");
    process.exit(2);
  }
  const results = await check({ dist: values.dist, urls: values.url });
  for (const [page, problems] of results) {
    for (const problem of problems) console.error(`${page}: ${problem}`);
  }
  if (results.size > 0) process.exit(1);
  console.log("Every page holds its first paint until its stylesheets and fonts are ready.");
}
