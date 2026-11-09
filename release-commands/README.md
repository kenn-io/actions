# Released commands

Check README, docs, and installer examples against the latest published GitHub release. The action downloads one Linux amd64 archive and asks its binaries for help. It fails on an unknown command, child command, flag, or missing binary, with the source file and line.

## Add the check

```yaml
name: Released commands
on:
  pull_request:
    paths: [README.md, scripts/install.sh, scripts/install.ps1]
  release:
    types: [published]
permissions:
  contents: read
jobs:
  release-commands:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7.0.1
        with:
          persist-credentials: false
      - uses: kenn-io/actions/release-commands@main
        with:
          files: |
            README.md
            scripts/install.sh
            scripts/install.ps1
          binaries: kata
          asset: kata_*[0-9]_linux_amd64.tar.gz
```

Reference the action at `@main`, and pin it with the calling repository's `actions.lock`. List each file and binary on its own line. The asset glob must match exactly one `.tar.gz`, excluding alternate builds such as `_homebrew_` archives. `repository` defaults to the calling repository. `github-token` defaults to the job token; private releases need a token that can read them.

A flag named alone or with an empty assignment checks its spelling without validating a value.

## Examples it reads

In `.md` files, the checker reads fenced blocks, inline code, and tables containing a listed binary. A `$ ` prompt and `VAR=value` prefixes are allowed. Backslash continuations and indented optional synopsis lines join the preceding command. Shell comments end a command. Spaced pipes, `&&`, and `;` preserve each invocation of a listed binary. Quoted arguments stay together. Synopses such as `kata issue get|show <id> [--limit <n>]` check both spellings and the optional flag. Unspaced `a|b` words are alternatives. Unquoted `<…>` placeholders become a fixed word for parsing; generic `<command>`, `<subcommand>`, and ellipses are omitted.

Installer examples are messages printed by `echo`, `printf`, PowerShell output commands, or functions in the script that wrap them. They begin with a listed binary or introduce it after a colon, such as `echo "Get started: kata issue list"`. Shell `echo` arguments are joined, helpers substitute their positional arguments, and `printf` formatting is expanded. `Write-Host` arguments and array items are joined with its `-Separator`, a space by default; for other output commands each argument and array item is a separate message. Messages whose text doesn't parse as a shell command are treated as prose. PowerShell files require `pwsh` on `PATH`, as provided by GitHub's Ubuntu runners; the checker uses its syntax parser without executing the installer. Installer syntax or message-expansion errors fail the file with `cannot read installer messages`.

## Mark unreleased commands

Put a visible paragraph before a block that readers need before the next release:

````markdown
Not in the latest release yet

```sh
kata future-command
```
````

The note applies to the next Markdown block, including a whole list or table. A heading consumes the note without skipping the examples after it. The action checks that block but suppresses its findings. When every command in a marked block succeeds, it names the marker to remove, such as `passes; remove unreleased note` or `passes; remove expected-failure marker`.

For an example that intentionally fails, put this comment before its block or paragraph:

````markdown
<!-- release-commands: expected-failure -->

```sh
kata unknown-command
```
````

## Fix a finding

A finding looks like `README.md:42: kata future-command: v1.0.0 has no command "future-command"`. Use a command the release supports, publish a release containing it, or mark the block as unreleased. A missing binary finding means the downloaded archive doesn't contain the documented executable.

## Limits

The action checks Linux amd64 binaries only. It asks each binary for `help <command path>`, then checks command and visible flag names against the final command's help page, including inherited flags. Hidden flags count as absent. Values only determine which words belong to flags; the checker doesn't validate types, argument counts, data, permissions, or deprecation notices. Each help invocation has a fresh temporary home, a restricted environment, and a ten-second limit. The release binary must be trusted.

Command discovery reads Cobra's `Usage:` section and flag rows, including inherited flags and kata's comma-separated `Global:` line. Unrecognized flag rows produce an `unsupported help` error. A command can accept positional arguments and also offer child commands. A group whose usage only shows `[flags]` and `[command]` rejects an unknown child. Commands with a different help format need a separate check. Files must be listed explicitly; the action doesn't crawl links or infer which pages a site publishes. Installer parsing reads messages without evaluating shell or PowerShell scripts. Shell variables outside printer helpers remain symbolic; other variables inside helpers are omitted. Command substitutions receive a placeholder. Commands behind `sudo`, `./`, or other wrappers aren't checked. The checker is built once and runs from the workspace, so findings use the relative paths supplied in `files`.

## Run locally

Download the release's Linux amd64 archive, then run from this directory:

```sh
go run . --release v1.0.0 --archive /tmp/kata_1.0.0_linux_amd64.tar.gz --binary kata /path/to/README.md
```

Repeat `--binary` for each executable the docs mention.
