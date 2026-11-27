# Test report

This action saves a test run's JSON report as a workflow artifact. Monitoring
downloads these artifacts to find tests that failed and then passed on a retry,
and to follow how long each package's tests take. Use it after every test step,
whether the step passed or failed, so every run's timings are available.

## What it uploads

One artifact named `<kind>-<suite>-attempt-<run attempt>`, for example
`gotestsum-linux-race-attempt-1`. It holds:

- every file the `path` patterns match, laid out as `actions/upload-artifact`
  lays them out;
- `kenn-test-report.meta`, a one-line JSON file naming the job that ran the
  tests:

  ```json
  {"version":1,"kind":"gotestsum","suite":"linux-race","job_id":139201940086,"run_attempt":1}
  ```

  `job_id` is the job's check run ID, which is also its ID in the GitHub
  Actions REST API, so a reader can look up the job's runner labels and result.
  The action writes the file into the deepest directory that holds every
  matched report, so the archive's layout does not change.

The action's `artifact-name` and `artifact-id` outputs name the upload. When no
pattern matches a file, as when the test step never ran, the action uploads
nothing and both outputs are empty. GitHub may not serve the artifact to the job
that uploaded it; download it from a later job.

## Use it

Run gotestsum with `--jsonfile`, then call the action with `if: always()`, so a
failed test step still uploads its report.

```yaml
      - name: Test
        run: |
          go tool gotestsum --format pkgname-and-test-fails \
            --jsonfile gotestsum/linux-race.json \
            --rerun-fails=2 --rerun-fails-report gotestsum/linux-race-reruns.txt \
            --packages ./... -- -race -shuffle=on
      - uses: kenn-io/actions/test-report@main
        if: always()
        with:
          suite: linux-race
          path: gotestsum/linux-race*.json
```

| Input | Default | Meaning |
| --- | --- | --- |
| `suite` | required | Names the tests this report covers. Keep it the same across runs and run attempts: history is kept per suite, and a retry is matched to its earlier attempt by suite. A matrix job needs a suite per matrix entry, such as `linux-${{ matrix.shard }}`. |
| `path` | required | Report files, one pattern per line, as for `actions/upload-artifact`. Patterns may be relative to the workspace or absolute, but not a mix. |
| `kind` | `gotestsum` | Report format: `gotestsum` for `go test -json` events, `vitest` or `playwright` for their JSON reporters. |
| `retention-days` | `30` | Days to keep the artifact. |

Each artifact name must be unique within a workflow run, so two calls with the
same kind and suite in one run fail. Remove any other upload step that uses the
same name.

The suite cannot contain `"`, `\`, `/`, `:`, `<`, `>`, `|`, `*`, `?`, or
control characters. The action needs `job.check_run_id`, which GitHub
Enterprise Server does not provide.
