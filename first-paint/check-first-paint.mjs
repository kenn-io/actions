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

/** @typedef {import("parse5").DefaultTreeAdapterMap["element"]} Element */
/** @typedef {import("parse5").DefaultTreeAdapterMap["node"]} Node */
/** @typedef {(url: URL) => Promise<string | undefined>} Load */

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

/** @param {Node} node @returns {Generator<Element>} */
function* elements(node) {
  if ("tagName" in node) yield node;
  const children = "content" in node ? node.content.childNodes : "childNodes" in node ? node.childNodes : [];
  for (const child of children) yield* elements(child);
}

const attr = (/** @type {Element} */ el, /** @type {string} */ name) =>
  el.attrs.find((a) => a.name === name)?.value;

const text = (/** @type {Element} */ el) =>
  el.childNodes.map((child) => ("value" in child ? child.value : "")).join("");

const isStylesheet = (/** @type {Element} */ el) => {
  if (el.tagName !== "link") return false;
  const rel = (attr(el, "rel") ?? "").toLowerCase().split(/\s+/);
  return rel.includes("stylesheet") && !rel.includes("alternate");
};

// A stylesheet a script swaps in after it loads, such as the media="print"
// onload trick or a preload whose onload sets rel, never blocks first paint.
const isSwapped = (/** @type {Element} */ el) => {
  const rel = (attr(el, "rel") ?? "").toLowerCase().split(/\s+/);
  const style = isStylesheet(el) || (rel.includes("preload") && attr(el, "as")?.toLowerCase() === "style");
  return el.tagName === "link" && style && attr(el, "onload") !== undefined;
};

// A print-only stylesheet never styles the screen, so it cannot flash.
const isPrint = (/** @type {Element} */ el) => (attr(el, "media") ?? "").trim().toLowerCase() === "print";

const QUOTED = String.raw`(?:"([^"]*)"|'([^']*)')`;
const URL_TOKEN = String.raw`url\(\s*(?:"([^"]*)"|'([^']*)'|([^)\s]*))\s*\)`;
const firstGroup = (/** @type {RegExpMatchArray} */ match) => match.slice(1).find((group) => group !== undefined) ?? "";

/**
 * Lists problems in one stylesheet and the stylesheets it imports.
 * @param {string} css @param {URL} base @param {Load} load @param {Set<string>} seen
 * @returns {Promise<string[]>}
 */
async function checkCss(css, base, load, seen) {
  const problems = [];
  const source = css.replace(/\/\*[\s\S]*?\*\//g, "");
  for (const [face] of source.matchAll(/@font-face\s*\{[^}]*\}/gi)) {
    for (const match of face.matchAll(new RegExp(URL_TOKEN, "g"))) {
      const url = firstGroup(match);
      if (!url.startsWith("data:")) {
        problems.push(`font ${url} in ${base.pathname} loads as a separate file; embed it as a data: URL`);
      }
    }
  }
  for (const match of source.matchAll(new RegExp(String.raw`@import\s+(?:${URL_TOKEN}|${QUOTED})`, "gi"))) {
    problems.push(...(await checkSheet(new URL(firstGroup(match), base), base, load, seen)));
  }
  return problems;
}

/**
 * @param {URL} url @param {URL} from @param {Load} load @param {Set<string>} seen
 * @returns {Promise<string[]>}
 */
async function checkSheet(url, from, load, seen) {
  if (url.origin !== from.origin) {
    return [`stylesheet ${url.href} is on another origin; serve it with the site and embed its fonts`];
  }
  if (seen.has(url.href)) return [];
  seen.add(url.href);
  const css = await load(url);
  if (css === undefined) return [`stylesheet ${url.pathname} was not found`];
  return checkCss(css, url, load, seen);
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
  const hold = all.findIndex(
    (el) => el.tagName === "script" && attr(el, "src") === undefined && inHead.has(el) && code(text(el)) === HOLD,
  );
  /** @type {Set<string>} */
  const seen = new Set();
  let first = -1;
  for (const [index, el] of all.entries()) {
    if (isSwapped(el)) {
      problems.push(`stylesheet ${attr(el, "href") ?? ""} is swapped in by an onload handler; link it with a plain <link rel="stylesheet">`);
    } else if (el.tagName === "style") {
      if (first === -1) first = index;
      problems.push(...(await checkCss(text(el), page, load, seen)));
    } else if (isStylesheet(el) && !isPrint(el)) {
      if (first === -1) first = index;
      const href = attr(el, "href") ?? "";
      if (!inHead.has(el)) problems.push(`stylesheet ${href} is outside <head>`);
      problems.push(...(await checkSheet(new URL(href, page), page, load, seen)));
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
  return response.ok ? response.text() : undefined;
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
      return readFile(file, "utf8").catch(() => undefined);
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
    const page = new URL(url);
    const html = await fetchText(page);
    const problems = html === undefined ? ["page returned an error"] : await checkPage(html, page, fetchText);
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
