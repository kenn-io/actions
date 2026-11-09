package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExamples(t *testing.T) {
	type expectedExample struct {
		text string
		line int
	}
	type expectedBlock struct {
		commands []expectedExample
		marker   string
	}
	for _, test := range []struct {
		name, ext, source string
		want              []expectedBlock
	}{
		{"powershell here-string apostrophe", ".ps1", "Write-Host @'\nDon't\nWrite-Host 'stub nope'\n'@\nWrite-Host 'stub run'", []expectedBlock{{commands: []expectedExample{{"stub run", 5}}}}},
		{"printf helper unquoted parameter", ".sh", `info() { printf '%s\n' $1; }; info 'stub nope'`, []expectedBlock{{commands: []expectedExample{{"stub", 1}}}}},
		{"prompted bracketed output", ".md", "```console\n$ stub plain\n  [\"--bogus\"]\n```", []expectedBlock{{commands: []expectedExample{{"stub plain", 2}}}}},
		{"joined echo operands", ".sh", `echo 'stub plain' 'stub nope'`, []expectedBlock{{commands: []expectedExample{{"stub plain stub nope", 1}}}}},
		{"printf helper literal text", ".sh", `info() { printf 'stub nope\n%s\n' "$1"; }; info Ready`, []expectedBlock{{commands: []expectedExample{{"stub nope", 1}}}}},
		{"printf unused operand", ".sh", `printf 'stub plain\n' 'stub nonexistent'`, []expectedBlock{{commands: []expectedExample{{"stub plain", 1}}}}},
		{"printf helper unused operand", ".sh", `info() { printf 'Ready\n' "$1"; }; info 'stub nonexistent'`, nil},
		{"printf helper message operand", ".sh", `info() { printf '%s\n' "$1"; }; info 'stub nope'`, []expectedBlock{{commands: []expectedExample{{"stub nope", 1}}}}},
		{"shell escaped double quotes", ".sh", `echo "Run: stub plain --log.level \"a --bogus\""`, []expectedBlock{{commands: []expectedExample{{`stub plain --log.level "a --bogus"`, 1}}}}},
		{"powershell doubled apostrophes", ".ps1", `Write-Host 'Run: stub plain --log.level ''a --bogus'''`, []expectedBlock{{commands: []expectedExample{{`stub plain --log.level 'a --bogus'`, 1}}}}},
		{"printf substituted format", ".sh", `printf 'stub %s\n' plain`, []expectedBlock{{commands: []expectedExample{{"stub plain", 1}}}}},
		{"parenthesized powershell output", ".ps1", "Write-Host 'stub plain'\n(Write-Host 'stub nonexistent')", []expectedBlock{{commands: []expectedExample{{"stub plain", 1}, {"stub nonexistent", 2}}}}},
		{"powershell pipeline chain", ".ps1", `Test-Path . && Write-Host 'stub nope'`, []expectedBlock{{commands: []expectedExample{{"stub nope", 1}}}}},
		{"printf escaped backslash before n", ".sh", `printf 'Next: stub plain C:\\new --bogus\n'`, []expectedBlock{{commands: []expectedExample{{`stub plain C:\new --bogus`, 1}}}}},
		{"printf tab between command words", ".sh", `printf 'stub\tplain --bogus'`, []expectedBlock{{commands: []expectedExample{{"stub\tplain --bogus", 1}}}}},
		{"shell transformed helper", ".sh", `info() { echo "${1:+Ready}"; }; info 'stub nope'`, nil},
		{"shell nested transformed helper", ".sh", `info() { echo "${x:-$1}"; }; info 'stub nope'`, nil},
		{"powershell transformed helper", ".ps1", "function info([string]$Message) { Write-Host $Message.Length }\ninfo 'stub nope'", nil},
		{"shell colon prefixed helper", ".sh", `info() { echo "Get started: $1"; }; info 'stub nope && stub plain'`, []expectedBlock{{commands: []expectedExample{{"stub nope && stub plain", 1}}}}},
		{"shell static double quoted backslashes", ".sh", `echo "stub run C:\\new"`, []expectedBlock{{commands: []expectedExample{{`stub run C:\new`, 1}}}}},
		{"printf double quoted newline", ".sh", `printf "stub nope\n"`, []expectedBlock{{commands: []expectedExample{{"stub nope", 1}}}}},
		{"printf prefixed newlines", ".sh", `printf '\nGet started:\n  stub group nope\n'`, []expectedBlock{{commands: []expectedExample{{"stub group nope", 1}}}}},
		{"synopsis after comment", ".md", "```sh\nstub plain file # note\n    [--bogus]\n```", []expectedBlock{{commands: []expectedExample{{"stub plain file  [--bogus]", 2}}}}},
		{"powershell commented helper", ".ps1", "<#\nfunction info([string]$Message) { Write-Host $Message }\n#>\ninfo 'stub nope'", nil},
		{"powershell block comment output call", ".ps1", "<#\nWrite-Host 'stub nope'\n#>", nil},
		{"powershell quoted output call", ".ps1", `Write-Host "Details; Write-Host 'stub nope'"`, nil},
		{"powershell ignored parameter", ".ps1", "function Write-Info([string]$Message) { Write-Host 'Ready' }\nWrite-Info 'stub nope'", nil},
		{"shell adjacent operator", ".sh", `true&&echo 'stub nope'`, []expectedBlock{{commands: []expectedExample{{"stub nope", 1}}}}},
		{"shell helper does not forward", ".sh", `check() { echo 'ready'; test -n "$1"; }; check 'stub nope'`, nil},
		{"shell helper forwards first argument", ".sh", `info() { echo "$1"; }; info 'stub run' 'stub nope'`, []expectedBlock{{commands: []expectedExample{{"stub run", 1}}}}},
		{"console commands and output", ".md", "```console\n$ stub run file\nstub nope\n$ stub group child ID \\\n  --bogus\nstub started\n```", []expectedBlock{{commands: []expectedExample{{"stub run file", 2}, {"stub group child ID  --bogus", 4}}}}},
		{"shell static branches", ".sh", `if x; then echo 'stub run'; else echo 'stub nope'; fi`, []expectedBlock{{commands: []expectedExample{{"stub run", 1}, {"stub nope", 1}}}}},
		{"shell case arm", ".sh", "case x in\n  *) echo 'stub nope' ;;\nesac", []expectedBlock{{commands: []expectedExample{{"stub nope", 2}}}}},
		{"powershell tags", ".ps1", `Write-Information 'Ready' -Tags 'stub nope'`, nil},
		{"shell redirect substitutions", ".sh", "info() { echo ready > \"$(echo \"$1\")\"; }; info 'stub nope'\ncat <<EOF\n$(echo 'stub nope')\ndon't\nEOF\necho 'stub run'", []expectedBlock{{commands: []expectedExample{{"stub run", 6}}}}},
		{"version output beside command", ".md", "```text\nstub v0.38.0 (commit abc)\nstub nope\n```", []expectedBlock{{commands: []expectedExample{{"stub nope", 3}}}}},
		{"printf argument stays literal", ".sh", `printf '%s\n' 'stub run C:\new'`, []expectedBlock{{commands: []expectedExample{{`stub run C:\new`, 1}}}}},
		{"uncalled helper", ".sh", "unused() {\necho 'stub nope'\n}\necho 'stub run'", []expectedBlock{{commands: []expectedExample{{"stub nope", 2}, {"stub run", 4}}}}},
		{"shell non-output strings", ".sh", `grep 'stub nope' file; command='stub nope' # echo 'stub nope'`, nil},
		{"powershell non-output strings", ".ps1", "Select-String 'stub nope' file\n$command = 'stub nope'", nil},
		{"powershell computed output", ".ps1", "Write-Host (Get-Thing 'stub nope')", nil},
		{"powershell uncalled helper", ".ps1", "function unused {\nWrite-Host 'stub nope'\n}\nWrite-Host 'stub run'", []expectedBlock{{commands: []expectedExample{{"stub nope", 2}, {"stub run", 4}}}}},
		{"powershell printer wrapper", ".ps1", "function info([string]$Message) { Write-Host $Message -ForegroundColor Green }\ninfo 'Get started: stub nope && stub plain'", []expectedBlock{{commands: []expectedExample{{"stub nope && stub plain", 2}}}}},
		{"colon argument", ".sh", `echo 'stub run host:stub target'`, []expectedBlock{{commands: []expectedExample{{"stub run host:stub target", 1}}}}},
		{"powershell comments", ".ps1", "# Details; Write-Host 'stub nope'\nWrite-Host 'ready'", nil},
		{"table", ".md", "| Example |\n| --- |\n| `stub nope` |", []expectedBlock{{commands: []expectedExample{{"stub nope", 3}}}}},
		{"multiline echo", ".sh", "echo 'Next:\nstub nope'", nil},
		{"continuation", ".md", "```sh\nstub group child ID \\\n --bogus\n```", []expectedBlock{{commands: []expectedExample{{"stub group child ID  --bogus", 2}}}}},
		{"synopsis continuation", ".md", "```sh\nstub group child ID\n    [--bogus]\n```", []expectedBlock{{commands: []expectedExample{{"stub group child ID [--bogus]", 2}}}}},
		{"comment backslash", ".md", "```sh\nstub run file # note \\\nstub nope\n```", []expectedBlock{{commands: []expectedExample{{"stub run file # note " + "\\", 2}, {"stub nope", 3}}}}},
		{"literal backslashes", ".md", "```sh\nstub run file \\\\\nstub nope\n```", []expectedBlock{{commands: []expectedExample{{`stub run file \\`, 2}, {"stub nope", 3}}}}},
		{"html consumes unreleased note", ".md", "Not in the latest release yet\n\n<div>Spacer</div>\n\n`stub nope`", []expectedBlock{{commands: []expectedExample{{"stub nope", 5}}}}},
		{"blockquote note before outside block", ".md", "> Not in the latest release yet\n\n```sh\nstub nope\n```", []expectedBlock{{marker: "unreleased note", commands: []expectedExample{{"stub nope", 4}}}}},
		{"nested marker", ".md", "> Not in the latest release yet\n>\n> ```sh\n> stub nope\n> ```", []expectedBlock{{marker: "unreleased note", commands: []expectedExample{{"stub nope", 4}}}}},
		{"quoted newline", ".md", "```sh\nstub run 'hello\n  world'\n```", []expectedBlock{{commands: []expectedExample{{"stub run 'hello\n  world'", 2}}}}},
		{"expected failure comment", ".md", "<!-- release-commands: expected-failure -->\n\n`stub run file`", []expectedBlock{{marker: "expected-failure marker", commands: []expectedExample{{"stub run file", 3}}}}},
		{"unreleased note", ".md", "Not in the latest release yet\n\n`stub nope`", []expectedBlock{{marker: "unreleased note", commands: []expectedExample{{"stub nope", 3}}}}},
		{"mdx inline", ".mdx", "Use `stub nope`.", []expectedBlock{{commands: []expectedExample{{"stub nope", 1}}}}},
		{"uppercase markdown", ".MD", "`stub nope`", []expectedBlock{{commands: []expectedExample{{"stub nope", 1}}}}},
		{"note paragraph command", ".md", "Not in the latest release yet: `stub run file`", []expectedBlock{{marker: "unreleased note", commands: []expectedExample{{"stub run file", 1}}}}},
		{"markdown inline", ".markdown", "Use `stub nope`.", []expectedBlock{{commands: []expectedExample{{"stub nope", 1}}}}},
		{"powershell same line", ".ps1", "if ($true) { Write-Host 'stub nope' }; Write-Host 'stub run'", []expectedBlock{{commands: []expectedExample{{"stub nope", 1}, {"stub run", 1}}}}},
		{"interpolated shell message", ".sh", `echo "stub nope $name"`, nil},
		{"interpolated powershell message", ".ps1", `Write-Host "stub nope $name"`, nil},
		{"shell marker blank line", ".sh", "# Not in the latest release yet\necho 'stub future'\n\necho 'stub run'", []expectedBlock{{marker: "unreleased note", commands: []expectedExample{{"stub future", 2}}}, {commands: []expectedExample{{"stub run", 4}}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			blocks, err := examples("example"+test.ext, []byte(test.source), []string{"stub"})
			require.NoError(t, err)
			var got []expectedBlock
			for _, block := range blocks {
				projected := expectedBlock{marker: block.marker}
				for _, example := range block.commands {
					projected.commands = append(projected.commands, expectedExample{example.text, example.line})
				}
				got = append(got, projected)
			}
			require.Equal(t, test.want, got)
		})
	}
}

func TestExampleInvocations(t *testing.T) {
	blocks, err := examples("example.md", []byte("`stub plain`"), []string{"stub"})
	require.NoError(t, err)
	require.Equal(t, []exampleBlock{{commands: []example{{
		text: "stub plain", line: 1,
		invocations: [][]string{{"stub", "plain"}},
	}}}}, blocks)
}

func TestInstallerAssignment(t *testing.T) {
	messages, err := installerStrings([]byte(`printf '-v' result '%s' 'stub nope'`), false)
	require.NoError(t, err)
	require.Empty(t, messages)
}
