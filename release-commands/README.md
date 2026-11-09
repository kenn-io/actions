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

Run the action in its own job. It installs its own Go version.

Reference the action at `@main`, and pin it with the calling repository's `actions.lock`. Include `actions/setup-go@v7.0.0` in that lockfile because this action calls it. List each file and binary on its own line. The asset glob must match exactly one `.tar.gz`, excluding alternate builds such as `_homebrew_` archives. `repository` defaults to the calling repository. `github-token` defaults to the job token; private releases need a token that can read them.

A flag named alone or with an empty assignment checks its spelling without validating a value.

## Examples it reads

In `.md` files, the checker reads fenced blocks and inline code, including code spans in table cells. A `$ ` prompt and `VAR=value` prefixes are allowed. Backslash continuations and indented optional synopsis lines join the preceding command. Shell comments end a command. Spaced pipes, `&&`, and `;` preserve each invocation of a listed binary. Quoted arguments stay together. Synopses such as `kata issue get|show <id> [--limit <n>]` check both spellings and the optional flag. Unspaced `a|b` words are alternatives. Concrete angle-bracket placeholders such as `<shell>` stay symbolic. Command-path discovery stops at a placeholder; flags after a positional placeholder are checked against the current command, while flags belonging to an unknown child are left unchecked. Generic `<command>`, `<subcommand>`, `[command]`, and ellipses are omitted. Markdown candidates that do not parse as shell commands are treated as prose; a missing configured binary still fails at its source line.

Installer examples are quoted text passed to shell `echo` or `printf`, PowerShell `Write-*` calls, or simple printer wrappers defined in the same file. They begin with a listed binary or introduce it after a colon, such as `echo "Get started: kata issue list"`. The checker reads the text at the output call and preserves its source line. Shell `printf` format strings decode escapes, including `\n`, `\t`, and octal or hexadecimal ANSI codes. A command after an escaped newline points to the source line containing that escape. It strips leading color variables and ANSI codes. Strings passed to other commands, comments, and uncalled helper functions supply no examples. PowerShell messages must appear on one source line. The reader runs in Go and requires no PowerShell installation. Invalid shell syntax fails the file with `cannot read installer messages`.

## Mark unreleased commands

Put a visible paragraph before a block that readers need before the next release:

````markdown
Not in the latest release yet

```sh
kata future-command
```
````

The note applies to the next Markdown block, including a whole list or table. A heading consumes the note without skipping the examples after it. The action checks that block but suppresses its findings. When a marked block has at least one checked command and every checked command succeeds, it names the marker to remove, such as `passes; remove unreleased note` or `passes; remove expected-failure marker`. GitHub Actions also shows this notice as a file annotation.

For an output sample that resembles a command, put this comment before its block or paragraph:

````markdown
<!-- release-commands: expected-failure -->

```sh
kata started (pid 123)
```
````

Installer comments use `# Not in the latest release yet` or `# release-commands: expected-failure`. A comment covers the messages printed from the lines below it, up to the next blank source line. The check reports the same removal notice when every command in that block passes. Use the expected-failure comment for prose that starts with a binary name.

## Fix a finding

Findings first suggest the visible `Not in the latest release yet` note for unreleased commands, with `#` before it in installers. Use a command the release supports, publish a release containing it, or add that note. Reserve `<!-- release-commands: expected-failure -->` and its installer form for output samples. Unquoted trailing shell comments stay out of findings. A finding starts with `README.md:42: kata future-command: v1.0.0 has no command "future-command"`. A missing binary finding means the downloaded archive doesn't contain the documented executable.

## Limits

The action checks Linux amd64 binaries only. It asks each binary for `help <command path>`, then checks command and visible flag names against the final command's help page, including inherited flags. Hidden flags count as absent. Values only determine which words belong to flags; the checker doesn't validate types, argument counts, data, permissions, or deprecation notices. Successful, parseable help output is reused across files within one run for each binary and exact help path. Failed or unparseable help is retried. Each actual help invocation has a fresh temporary home, a restricted environment, and a ten-second limit. The release binary must be trusted.

Command discovery reads Cobra's `Usage:` section and flag rows, including inherited flags and kata's comma-separated `Global:` line. Unrecognized flag rows produce an `unsupported help` error. A command can accept positional arguments and also offer child commands. A group whose usage only shows `[flags]` and `[command]` rejects an unknown child. Commands with a different help format need a separate check. Files must be listed explicitly; the action doesn't crawl links or infer which pages a site publishes. Installer parsing reads static quoted text without evaluating scripts. Computed messages, commands split across arguments or output calls, argument escape expansion, and shell heredocs aren't evaluated. Only shell `printf` format strings decode escapes; format conversions such as `%s` stay symbolic. Helper calls must name the function directly; the reader doesn't resolve dynamic calls or evaluate branch conditions. `printf -v` assignments supply no output. Keep each command example in one quoted argument at a supported output call. Commands behind `sudo`, `./`, or other wrappers aren't checked. The checker is built once and runs from the workspace, so findings use the relative paths supplied in `files`.

## Run locally

Download the release's Linux amd64 archive, then run from this directory:

```sh
go run . --release v1.0.0 --archive /tmp/kata_1.0.0_linux_amd64.tar.gz --binary kata /path/to/README.md
```

Repeat `--binary` for each executable the docs mention.
