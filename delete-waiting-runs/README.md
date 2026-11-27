# Delete waiting runs

Delete runs waiting for approval when a fork pull request closes. The action
matches the pull request's head branch and repository and requires `actions: write`.
It deletes every matching run, including runs shared with another pull request.
When the head repository is absent, it matches every run on that branch from the
head owner, including runs from the owner's other repositories. If both the
repository and owner are absent, it fails. Deleted runs no longer appear in the
Actions tab. The action does not check out pull request code.

Use a Linux runner with GNU `date`, such as `ubuntu-latest`. GitHub limits a filtered search to 1,000 runs, so the action
splits large searches by creation time. If 1,000 or more runs share one second,
it fails without deleting runs.

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
