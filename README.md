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
- Lets a lock file refresh (`lockFileMaintenance`) for npm, pnpm, Yarn, or Bun
  open without a release timestamp. A refresh has no timestamp of its own, so
  with the timestamp requirement it would sit on a pending check forever. The
  package manager holds the refresh to the wait instead, as
  [Package manager release age](#package-manager-release-age) describes.
- Turns off the Dependency Dashboard. Its "Pending Status Checks" checkboxes
  open a pull request for an unaged release right away, which defeats the wait.
- Removes the hourly limit on new Renovate pull requests (`prHourlyLimit`).
  Renovate's default of 2 an hour stops it from creating a new branch once 2
  Renovate pull requests have opened that hour. A weekly schedule gets about
  one run in its window, so a repository with more than 2 groups to update
  opened only 2 a week, and the same groups lost every week. One pull request
  per group already keeps the count small.
- Excludes TypeScript `7.0.x`, including prereleases, because it lacks the
  compiler API that `svelte-check` needs. Later `7.x` versions remain eligible.
  The rule matches both the dependency name and package name `typescript`, so
  explicit aliases such as `"@typescript/native": "npm:typescript@7.0.2"` remain
  eligible for updates.

Repositories extending this preset from `main` automatically inherit the
constraint on their next Renovate run after it merges. Equivalent local
`7.0.x` exclusions can then be removed. Stricter local `<7` overrides remain
valid and should stay in place.

Keep the release-age settings in this preset. A repository does not override
them, and it does not turn the Dependency Dashboard back on. When Renovate
cannot finish an update, such as a version with a checksum it cannot update,
the repository keeps the update enabled and says in the rule's `description`
what to fix on the branch before merging.

### Package manager release age

Renovate's wait covers the versions it proposes. It does not cover the
transitive packages a package manager picks while it rewrites a lock file, or
anything a person installs by hand. Every JavaScript project sets its package
manager's own 7-day limit next to its lock file:

| Package manager | File | Setting |
| --- | --- | --- |
| npm 11.10 or later | `.npmrc` | `min-release-age=7` (days) |
| Bun | `bunfig.toml` | `minimumReleaseAge = 604800` under `[install]` (seconds) |
| pnpm 10.16 or later | `pnpm-workspace.yaml` | `minimumReleaseAge: 10080` (minutes) |

Renovate gives npm a `--before` date of its own, but it drops that date without
failing when an exact pin is too young. With `min-release-age` in `.npmrc`,
Renovate leaves the limit to npm. Renovate gives Bun and pnpm no limit at all.

The limit applies only when the package manager picks versions. A frozen
install, such as `npm ci` or `bun install --frozen-lockfile`, installs the
locked versions as they are.

A security fix younger than 7 days needs an exclusion for the fixed package,
removed once the release is 7 days old: `min-release-age-exclude[]=<name>` in
`.npmrc` (npm 11.17 or later), `minimumReleaseAgeExcludes` in `bunfig.toml`, or
`minimumReleaseAgeExclude` in `pnpm-workspace.yaml`. A project that installs a
`@kenn-io/*` package from a registry excludes those packages the same way, or
the package manager refuses a kenn-io release the preset exempts.

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
