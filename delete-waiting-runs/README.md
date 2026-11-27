# Delete waiting runs

Delete runs waiting for approval when a fork pull request closes. The action
matches the pull request's head branch and repository and requires `actions: write`.
Deleted runs no longer appear in the Actions tab. The action does not check out
pull request code.

```yaml
name: Delete waiting runs
on:
  pull_request_target:
    types: [closed]
permissions:
  actions: write
jobs:
  delete:
    if: github.event.pull_request.head.repo.full_name != github.repository
    runs-on: ubuntu-latest
    steps:
      - uses: kenn-io/actions/delete-waiting-runs@main
```

Reference the action at `@main` and run `gh actions-lock` to pin it.
