# Changes

Run jobs for their changed paths. Unknown paths and events other than
`pull_request` set `all=true`, so new files keep full coverage.

`filters` is required YAML in [dorny/paths-filter syntax](https://github.com/dorny/paths-filter).
Reserve `all` for paths that need every job. Outputs are `groups`, a JSON array
of matching names, and `all`, the string `true` or `false`.

Pin to a full commit on this repository's `main`.

```yaml
jobs:
  changes:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      pull-requests: read
    outputs:
      groups: ${{ steps.changes.outputs.groups }}
      all: ${{ steps.changes.outputs.all }}
    steps:
      - uses: kenn-io/actions/changes@<full commit SHA on main>
        id: changes
        with:
          filters: |
            go: ['**/*.go']
            docs: ['**/*.md']
            all: ['.github/**', 'go.mod', 'go.sum']
  test:
    needs: changes
    if: ${{ !cancelled() && (needs.changes.result != 'success' || needs.changes.outputs.all == 'true' || contains(fromJSON(needs.changes.outputs.groups || '[]'), 'go')) }}
    runs-on: ubuntu-latest
    steps:
      - run: echo 'Run Go checks here'
```

The condition runs checks if detection fails.
