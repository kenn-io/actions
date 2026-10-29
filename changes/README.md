# Changes

Select CI lanes from changed paths. Each filter lists paths safe to skip in one
negated brace pattern. Positive exceptions keep inputs a lane still consumes.
An unknown path matches every lane whose skip set leaves it out.

`filters` must be a named YAML mapping in [dorny/paths-filter syntax](https://github.com/dorny/paths-filter).
Without `:` or a newline, dorny reads a bare string as a filter-file path.
Reserve `changes`, which dorny uses for its output. A name collision returns
`groups=[]` and `all=true`. `groups` is a JSON array of
matching names. `all` is `true` for events other than `pull_request` and for
pull requests with at least 3,000 changed files.

Pin to a full commit on this repository's `main`. Grant `pull-requests: read`,
and `contents: read` when the job also checks out code.

```yaml
jobs:
  changes:
    runs-on: ubuntu-latest
    permissions:
      pull-requests: read
    outputs:
      groups: ${{ steps.changes.outputs.groups }}
      all: ${{ steps.changes.outputs.all }}
    steps:
      - uses: kenn-io/actions/changes@<full commit SHA on main>
        id: changes
        with:
          filters: |
            go: ['!{frontend/**,docs/**,*.md}', 'docs/embedded.md']
  test:
    needs: changes
    if: ${{ !cancelled() && (needs.changes.result != 'success' || needs.changes.outputs.all == 'true' || contains(fromJSON(needs.changes.outputs.groups), 'go')) }}
    runs-on: ubuntu-latest
    steps:
      - run: echo 'Run Go checks here'
```

Use one combined negated pattern per lane. With the default `some` quantifier,
separate negated patterns also match paths skipped by another pattern.
For one path, use `!docs/**`; a one-entry brace such as `!{docs/**}` does not expand.
The condition runs checks if detection fails.

GitHub lists at most 3,000 changed files. At that count or above, this action
skips filtering and sets `all=true` so a partial file list cannot skip a lane.
