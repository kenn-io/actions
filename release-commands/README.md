# Released commands

Check README, docs, and installer examples against the latest published GitHub release. The action downloads one Linux amd64 archive and asks its binaries for help. It fails on an unknown command, child command, flag, or missing binary, with the source file and line.

## Add the check

```yaml
name: Released commands
on:
  pull_request: {}
  merge_group: {types: [checks_requested]}
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

Select this job as a required status check for `main`.

Reference the action at `@main`, and pin it with the calling repository's `actions.lock`. Include `actions/setup-go@v7.0.0` in that lockfile because this action calls it. List each file and binary on its own line. The asset glob must match exactly one `.tar.gz`, excluding alternate builds such as `_homebrew_` archives. Binaries must be regular files at the archive root. `repository` defaults to the calling repository. `github-token` defaults to the job token; private releases need a token that can read them.

A flag named alone or with an empty assignment checks its spelling without validating a value.

## Examples it reads

In `.md` files, the checker reads fenced blocks and inline code, including code spans in table cells. `VAR=value` prefixes are allowed. Backslash continuations and indented optional synopsis lines join the preceding command. Spaced pipes, `&&`, and `;` preserve each invocation of a listed binary. Quoted arguments stay together. Synopses such as `kata issue get|show <id> [--limit <n>]` check both spellings and the optional flag. Unspaced `a|b` words are alternatives. Concrete angle-bracket placeholders such as `<shell>` stay symbolic. Command-path discovery stops at a placeholder; flags after a positional placeholder are checked against the current command, while flags belonging to an unknown child are left unchecked. Generic `<command>`, `<subcommand>`, `[command]`, and ellipses are omitted. Markdown candidates that do not parse as shell commands, or that pass a command or process substitution as an argument, are treated as prose.

Installer examples come from static shell `echo` and `printf` calls, shell helpers that forward their first argument to those printers, and PowerShell `Write-*` calls that start a line, helpers included, with a quoted message first. `echo` joins its operands with spaces; `printf` renders its format once with its operands. Each message keeps the output call's source line.

Messages must begin with a listed binary or introduce it after a colon, such as `info "Get started: kata issue list"`. Once the command begins, its arguments and later invocations stay together. PowerShell parenthesized messages, arrays, and options before the message are skipped; metadata after it supplies no examples. Shell helpers that call other helpers supply no examples. Computed output and messages that don't parse as shell commands are skipped. A listed installer with no recognized command for a configured binary fails with its file path. The scanner runs in Go and requires no PowerShell installation.

## Mark unreleased commands

Put a visible paragraph before a block that readers need before the next release:

````markdown
Not in the latest release yet

```sh
kata future-command
```
````

The note applies to the next Markdown block, including a whole list or table. A heading consumes the note without skipping the examples after it. The action checks that block but suppresses its findings. When a marked block has at least one checked command and every checked command succeeds, it names the marker to remove, such as `passes; remove unreleased note` or `passes; remove expected-failure marker`. The notice includes the file and line.

For an output sample that resembles a command, put this comment before its block or paragraph:

````markdown
<!-- release-commands: expected-failure -->

```sh
kata started (pid 123)
```
````

Installer comments use `# Not in the latest release yet`. A comment covers the messages printed from the lines below it, up to the next blank source line. The check reports the same removal notice when every command in that block passes.

## Fix a finding

Use a command the release supports, publish a release containing it, or add the visible `Not in the latest release yet` note, with `#` before it in installers. Reserve `<!-- release-commands: expected-failure -->` for output samples. Unquoted trailing shell comments stay out of findings. A finding starts with `README.md:42: kata future-command: v1.0.0 has no command "future-command"`. A missing binary finding means the downloaded archive doesn't contain the documented executable.

## Limits

Raw HTML `<pre>` blocks aren't read. Use Markdown fenced blocks for command examples.

Executable names must match `binaries` exactly. A Windows spelling such as `stub.exe` isn't checked when `binaries` lists `stub`.

The action checks Linux amd64 binaries only. It asks each binary for `help <command path>`, then checks command and visible flag names against the final command's help page, including inherited flags. Hidden flags count as absent. Commands that forward flags to another program need a separate check when those flags aren't listed in help. Values only determine which words belong to flags; the checker doesn't validate types, argument counts, data, permissions, or deprecation notices. Successful, parsed help is reused across files within one run for each binary and exact help path. Failed or unparseable help is retried. Each actual help invocation has a fresh temporary home, `PATH=/usr/bin:/bin` as its only other environment variable, and a ten-second limit. The release binary must be trusted.

Command discovery reads Cobra's `Usage:` section, `Flags:`, `Global Flags:`, and `Inherited Flags:` rows, roborev's compact `Global:` line, and msgvault's compact `Global Flags:` line. Compact global names use the value and shorthand definitions from root help; names absent from the compact list stay absent. Unrecognized flag sections produce an `unsupported help` error. Rows in another format are skipped, so their flags count as absent. A command can accept positional arguments and also offer child commands. A group whose usage only shows `[flags]` and `[command]` rejects an unknown child. Commands with a different help format need a separate check. Files must be listed explicitly; the action doesn't crawl links or infer which pages a site publishes. Installer parsing renders static shell calls and scans PowerShell source, comments included, without evaluating branches or resolving which functions run. Interpolated or computed messages, PowerShell messages containing `$`, multiline messages, and commands split across output calls aren't read. Marker detection reads source lines, so marker text inside a heredoc or multiline string can mark following output until a blank line. Commands behind `sudo`, `./`, or other wrappers aren't checked. The checker is built once and runs from the workspace, so findings use the relative paths supplied in `files`.

## Run locally

Download the release's Linux amd64 archive, then run from this directory:

```sh
go run . --release v1.0.0 --archive /tmp/kata_1.0.0_linux_amd64.tar.gz --binary kata /path/to/README.md
```

Repeat `--binary` for each executable the docs mention.
