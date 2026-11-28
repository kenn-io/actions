// @ts-check
// Prepares a test report artifact: finds the report files the way
// actions/upload-artifact finds them, and writes the metadata file naming the
// job into the archive's root directory, so the archive's layout does not
// change.
import { stat, writeFile } from "node:fs/promises";
import path from "node:path";
import { pathToFileURL } from "node:url";
import * as core from "@actions/core";
import * as glob from "@actions/glob";

export const META_FILE = "kenn-test-report.meta";
const KINDS = new Set(["gotestsum", "vitest", "playwright"]);

/**
 * Finds the files upload-artifact would upload for patterns, and the root
 * directory it would archive them under. Mirrors findFilesToUpload in
 * actions/upload-artifact's src/shared/search.ts, with hidden files excluded
 * as its default is.
 * @param {string} patterns
 * @returns {Promise<{ files: string[], root: string }>}
 */
export async function findReports(patterns) {
  const globber = await glob.create(patterns, {
    followSymbolicLinks: true,
    implicitDescendants: true,
    omitBrokenSymbolicLinks: true,
    excludeHiddenFiles: true,
  });
  const files = [];
  for (const found of await globber.glob()) {
    if (!(await stat(found)).isDirectory()) files.push(found);
  }
  const searchPaths = globber.getSearchPaths();
  let root = searchPaths[0] ?? "";
  if (searchPaths.length > 1) {
    root = commonAncestor(searchPaths);
  } else if (files.length === 1 && searchPaths[0] === files[0]) {
    root = path.dirname(files[0] ?? "");
  }
  return { files, root };
}

/**
 * The deepest directory every path is in, compared by path segment, as
 * upload-artifact's getMultiPathLCA computes it.
 * @param {string[]} paths
 */
export function commonAncestor(paths) {
  const split = paths.map((p) => path.normalize(p).split(path.sep));
  const shortest = Math.min(...split.map((parts) => parts.length));
  /** @type {string[]} */
  const common = paths[0]?.startsWith(path.sep) ? [path.sep] : [];
  for (let index = 0; index < shortest; index++) {
    const segment = split[0]?.[index];
    if (split.some((parts) => parts[index] !== segment)) break;
    common.push(segment ?? "");
  }
  return path.join(...common);
}

/**
 * Checks the action's inputs and returns the artifact name.
 * @param {{ kind: string, suite: string, jobId: string, attempt: string }} input
 */
export function artifactName({ kind, suite, jobId, attempt }) {
  if (!KINDS.has(kind)) throw new Error(`kind must be gotestsum, vitest, or playwright, not '${kind}'`);
  // Artifact names reject these characters; JSON.stringify escapes the rest.
  if (suite === "" || /[\x00-\x1f\x7f"\\/:<>|*?]/.test(suite)) {
    throw new Error(`suite must be printable and contain none of " \\ / : < > | * ?`);
  }
  if (!/^[1-9][0-9]*$/.test(jobId)) throw new Error("job.check_run_id is unavailable, so the report cannot name its job");
  return `${kind}-${suite}-attempt-${attempt}`;
}

/**
 * @param {{ kind: string, suite: string, jobId: string, attempt: string, patterns: string }} input
 * @returns {Promise<{ name: string, meta?: string }>} meta is unset when no report exists.
 */
export async function prepare(input) {
  const name = artifactName(input);
  const { files, root } = await findReports(input.patterns);
  if (files.length === 0) return { name };
  const meta = path.join(root, META_FILE);
  const record = { version: 1, kind: input.kind, suite: input.suite, job_id: Number(input.jobId), run_attempt: Number(input.attempt) };
  await writeFile(meta, `${JSON.stringify(record)}\n`);
  return { name, meta };
}

async function main() {
  try {
    const env = process.env;
    const { name, meta } = await prepare({
      kind: env.KIND ?? "",
      suite: env.SUITE ?? "",
      jobId: env.JOB_ID ?? "",
      attempt: env.RUN_ATTEMPT ?? "",
      patterns: env.REPORT_PATHS ?? "",
    });
    core.setOutput("name", name);
    if (meta === undefined) {
      core.info(`No ${env.KIND} report matched; nothing to upload for ${name}.`);
      core.setOutput("found", "false");
      return;
    }
    core.setOutput("meta", meta);
    core.setOutput("found", "true");
  } catch (error) {
    core.setFailed(error instanceof Error ? error.message : String(error));
  }
}

if (import.meta.url === pathToFileURL(process.argv[1] ?? "").href) await main();
