// @ts-check
import assert from "node:assert/strict";
import { mkdir, mkdtemp, readFile, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { test } from "node:test";
import { META_FILE, artifactName, commonAncestor, prepare } from "../prepare.mjs";

const input = { kind: "gotestsum", suite: "linux", jobId: "42", attempt: "2" };

/** @param {string[]} files */
async function reports(files) {
  const dir = await mkdtemp(path.join(tmpdir(), "test-report-"));
  for (const file of files) {
    await mkdir(path.dirname(path.join(dir, file)), { recursive: true });
    await writeFile(path.join(dir, file), "{}\n");
  }
  return dir;
}

/** @param {string} meta */
async function record(meta) {
  return JSON.parse(await readFile(meta, "utf8"));
}

test("the metadata names the job and lands in the archive's root", async () => {
  const dir = await reports(["gotestsum/a.json", "gotestsum/sub/b.json"]);
  const prepared = await prepare({ ...input, patterns: path.join(dir, "gotestsum/**/*.json") });
  assert.equal(prepared.name, "gotestsum-linux-attempt-2");
  assert.equal(prepared.meta, path.join(dir, "gotestsum", META_FILE), "the pattern's fixed directory is the root");
  assert.deepEqual(await record(prepared.meta ?? ""), { version: 1, kind: "gotestsum", suite: "linux", job_id: 42, run_attempt: 2 });
});

test("a directory names every report in it", async () => {
  const dir = await reports(["reports/nested/a.json"]);
  const prepared = await prepare({ ...input, patterns: path.join(dir, "reports") });
  assert.equal(prepared.meta, path.join(dir, "reports", META_FILE));
});

test("several patterns archive under their common directory", async () => {
  const dir = await reports(["gotestsum/a.json", "other/b.json"]);
  const prepared = await prepare({ ...input, patterns: `${path.join(dir, "gotestsum/*.json")}\n${path.join(dir, "other/*.json")}` });
  assert.equal(prepared.meta, path.join(dir, META_FILE));
});

test("a single named file archives under its own directory", async () => {
  const dir = await reports(["gotestsum/a.json"]);
  const prepared = await prepare({ ...input, patterns: path.join(dir, "gotestsum/a.json") });
  assert.equal(prepared.meta, path.join(dir, "gotestsum", META_FILE));
});

test("nothing to upload when no report exists, including excluded and hidden ones", async () => {
  const dir = await reports(["gotestsum/a.json", "gotestsum/.hidden/b.json"]);
  for (const patterns of [
    path.join(dir, "absent/*.json"),
    `${path.join(dir, "gotestsum/*.json")}\n!${path.join(dir, "gotestsum/a.json")}`,
    path.join(dir, "gotestsum/.hidden/*.json"),
  ]) {
    const prepared = await prepare({ ...input, patterns });
    assert.equal(prepared.meta, undefined, patterns);
  }
});

test("inputs that cannot name an artifact or a job are refused", () => {
  assert.throws(() => artifactName({ ...input, kind: "junit" }), /kind must be/);
  for (const suite of ["", "bad/suite", 'quote"', "line\nbreak"]) {
    assert.throws(() => artifactName({ ...input, suite }), /suite must be/, JSON.stringify(suite));
  }
  assert.equal(artifactName({ ...input, suite: "with space" }), "gotestsum-with space-attempt-2");
  assert.throws(() => artifactName({ ...input, jobId: "" }), /check_run_id is unavailable/);
});

test("the common directory compares whole path segments", () => {
  assert.equal(commonAncestor(["/a/bc/x", "/a/b/y"]), "/a");
  assert.equal(commonAncestor(["/a/b", "/a/b/c"]), "/a/b");
});
