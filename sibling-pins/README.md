# Sibling pins on main

This action fails a pull request or merge queue entry when the repository pins a
kenn-io sibling at a revision that is not on that sibling's `main` branch.

Sibling repositories squash-merge, so a pull request head never reaches the
sibling's `main`. Go can download such a commit only while the module proxy
still caches it, and a later clean build fails with `unknown revision`. While a
change spans two repositories, a pull request may pin the sibling's open pull
request. This check only blocks the merge.

## What it checks

The action reads these files anywhere in the candidate checkout, except under
`node_modules`:

- `go.mod`: every `go.kenn.io/*` requirement, direct or indirect, and every
  replacement that points at a `go.kenn.io` module. A replacement that points a
  sibling module at a fork fails.
- `package.json` and `bun.lock`: `github:kenn-io/...` and
  `git+https://github.com/kenn-io/...` dependencies.
- `Makefile`: `go run` and `go install` of `go.kenn.io/...@VERSION`.
- `.custom-gcl.yml`: `go.kenn.io` golangci-lint plugin versions.
- `.github/workflows/*.yml`, `action.yml`, and `action.yaml`:
  `uses: kenn-io/...@REF` steps and reusable workflows, including this action's
  own pin.

A pseudo-version names a commit. Any other Go version names a tag. The check
asks GitHub to compare each revision with the sibling's `main` and fails when
the revision has commits that `main` does not.

## Use it

Add a workflow that runs on `pull_request` and `merge_group`, then make its job
a required status check for `main`.

```yaml
name: Sibling pins on main

on:
  pull_request:
    types: [opened, synchronize, reopened, edited]
  merge_group:
    types: [checks_requested]

permissions:
  contents: read

jobs:
  sibling-pins:
    name: Sibling pins on main
    runs-on: ubuntu-latest
    timeout-minutes: 10
    steps:
      - uses: kenn-io/actions/sibling-pins@<commit on main>
```

- `pull_request` runs with the same permissions as normal pull request CI,
  including for forks. The action checks out only the candidate's dependency
  files and never runs the candidate's code. Do not use `pull_request_target`.
- Pin the action to a full commit on this repository's `main`.
- Keep the job name `Sibling pins on main`. The required status check matches
  that name.
- Run the action in its own job. It installs its own Go version.
- A pull request with no merge ref passes with a merge-conflicts notice. It
  cannot merge until the conflicts are resolved; that push runs the check,
  as does the merge queue.

### Private siblings

The job's own token can read public siblings only. A private sibling the token
cannot read fails with "has no revision ..., or this check cannot read the
repository". When a repository pins private siblings, pass a GitHub App token
with contents read access to them:

Fork pull request workflows do not receive repository secrets, so the App token
below is available only to same-repository pull requests and merge groups.

```yaml
    steps:
      - name: Read sibling repositories
        id: token
        uses: actions/create-github-app-token@64ab729a6294000f5aa4a4bc0b73d1f32bc45510 # v3
        with:
          app-id: ${{ vars.<APP_ID_VARIABLE> }}
          private-key: ${{ secrets.<APP_PRIVATE_KEY_SECRET> }}
          owner: kenn-io
          permission-contents: read
      - uses: kenn-io/actions/sibling-pins@<commit on main>
        with:
          github-token: ${{ steps.token.outputs.token }}
```

## Fix a finding

Move the pin to the sibling's merged commit on `main`.

- For a Go module, run `go get go.kenn.io/<repo>@<commit>` and then
  `go mod tidy`. An indirect requirement counts; Go downloads it the same way.
- For a frontend package, change the `#<commit>` in `package.json` and reinstall
  to update `bun.lock`.

## Run it locally

From the repository root, with `GH_TOKEN` or `GITHUB_TOKEN` set to a token that
can read the pinned siblings:

```sh
GH_TOKEN="$(gh auth token)" go run go.kenn.io/actions/sibling-pins@<commit> --root .
```

Go programs can import `go.kenn.io/actions/sibling-pins/siblingpins`, call
`Collect`, and pass the pins to `GitHub.Check` for structured findings.
