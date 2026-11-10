# kenn-io actions

Shared GitHub Actions for kenn-io repositories.

| Action | Purpose |
| --- | --- |
| [`changes`](changes/README.md) | Runs jobs for changed path groups, with full coverage for unknown paths. |
| [`first-paint`](first-paint/README.md) | Fails when a page can paint before its stylesheets or fonts are in place. |
| [`release-commands`](release-commands/README.md) | Checks documented commands and flags against the latest release. |
| [`sibling-pins`](sibling-pins/README.md) | Blocks a merge that pins a kenn-io sibling at a revision off that sibling's `main`. |

Reference each action at `@main`. The calling repository's
`.github/workflows/actions.lock` pins it to a commit; run `gh actions-lock` to
move that pin to the current `main`.

## Renovate release-age preset

[`renovate/release-age.json`](renovate/release-age.json) holds every
third-party release for 7 days before Renovate opens or updates a pull request
for it. Extend it from a repository's Renovate config:

```json
{
  "extends": ["config:recommended", "github>kenn-io/actions//renovate/release-age"]
}
```

The preset does the following:

- Sets `minimumReleaseAge` to 7 days with `internalChecksFilter: "strict"`, so
  Renovate creates no branch until a release has aged.
- Requires a release timestamp. Renovate holds a release whose registry gives no
  publish date, such as a Microsoft Container Registry image, until someone
  updates it by hand.
- Exempts kenn-io releases (`kenn-io/*` actions and tools, `go.kenn.io/*` and
  `github.com/kenn-io/*` modules, `@kenn-io/*` packages, and
  `ghcr.io/kenn-io/*` images) from the wait and from the repository's schedule.
- Exempts security fixes from the wait and the schedule.
- Lets an npm lock file refresh (`lockFileMaintenance`) open without a release
  timestamp. A refresh has no timestamp of its own, so with the timestamp
  requirement it would sit on a pending check forever. npm holds the refresh to
  the wait instead: a repository that turns on npm lock file maintenance must
  set `min-release-age` in each npm project's `.npmrc` and require npm 11.10 or
  later.
- Turns off the Dependency Dashboard. Its "Pending Status Checks" checkboxes
  open a pull request for an unaged release right away, which defeats the wait.

Keep the release-age settings in this preset. A repository does not override
them, and it does not turn the Dependency Dashboard back on. An update that
needs a person before it can merge, such as a version with a checksum Renovate
cannot update, gets `"enabled": false` in the repository's config and is updated
by hand.

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
