# kenn-io actions

Shared GitHub Actions for kenn-io repositories.

| Action | Purpose |
| --- | --- |
| [`changes`](changes/README.md) | Runs jobs for changed path groups, with full coverage for unknown paths. |
| [`first-paint`](first-paint/README.md) | Fails when a page can paint before its stylesheets or fonts are in place. |
| [`sibling-pins`](sibling-pins/README.md) | Blocks a merge that pins a kenn-io sibling at a revision off that sibling's `main`. |

Pin each action to a full commit on this repository's `main`.

## Development

Build and test with `-trimpath` so every checkout and git worktree shares one
Go build cache instead of compiling its own copy of every package. CI sets
`GOFLAGS=-trimpath`. Run `go env -w GOFLAGS=-trimpath` once to get the same
behavior locally, and use `GOFLAGS=-trimpath=false` to opt out for one command.

A trimmed binary does not record where its source files live, so Delve needs
help finding them:

- `dlv debug` and `dlv test` inherit `GOFLAGS`. Pass
  `--build-flags=-trimpath=false` so the debug build records real paths. Debug
  builds disable optimizations, so they never shared cache entries anyway.
- `dlv exec` and `dlv attach` on a trimmed binary: start Delve from the module
  directory and run `config substitute-path -guess` (Delve 1.24 or later). It
  maps the module path to your checkout, the standard library to `GOROOT`, and
  dependencies to the module cache.
