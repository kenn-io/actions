package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExamples(t *testing.T) {
	for _, test := range []struct {
		name, ext, source string
		want              []exampleBlock
	}{
		{"shell transformed helper", ".sh", `info() { echo "${1:+Ready}"; }; info 'stub nope'`, nil},
		{"shell nested transformed helper", ".sh", `info() { echo "${x:-$1}"; }; info 'stub nope'`, nil},
		{"powershell transformed helper", ".ps1", "function info([string]$Message) { Write-Host $Message.Length }\ninfo 'stub nope'", nil},
		{"shell literal prefixed helper", ".sh", `info() { echo "==> $1"; }; info 'stub nope'`, []exampleBlock{{commands: []example{{"stub nope", 1}}}}},
		{"shell static double quoted backslashes", ".sh", `echo "stub run C:\\new"`, []exampleBlock{{commands: []example{{`stub run C:\\new`, 1}}}}},
		{"printf double quoted newline", ".sh", `printf "stub nope\n"`, []exampleBlock{{commands: []example{{"stub nope", 1}}}}},
		{"printf prefixed newlines", ".sh", `printf '\nGet started:\n  stub group nope\n'`, []exampleBlock{{commands: []example{{"stub group nope", 1}}}}},
		{"printf flag newline", ".sh", `printf 'Next: stub plain --log.level\n'`, []exampleBlock{{commands: []example{{"stub plain --log.level", 1}}}}},
		{"synopsis after comment", ".md", "```sh\nstub plain file # note\n    [--bogus]\n```", []exampleBlock{{commands: []example{{"stub plain file  [--bogus]", 2}}}}},
		{"installer prose marker scope", ".sh", "# release-commands: prose\necho 'stub installed successfully'\necho 'stub ready'\n\necho 'stub nope'", []exampleBlock{{commands: []example{{"stub nope", 5}}}}},
		{"installer prose marker blank line", ".sh", "# release-commands: prose\n\necho 'stub nope'", []exampleBlock{{commands: []example{{"stub nope", 3}}}}},
		{"powershell commented helper", ".ps1", "<#\nfunction info([string]$Message) { Write-Host $Message }\n#>\ninfo 'stub nope'", nil},
		{"powershell block comment output call", ".ps1", "<#\nWrite-Host 'stub nope'\n#>", nil},
		{"powershell quoted output call", ".ps1", `Write-Host "Details; Write-Host 'stub nope'"`, nil},
		{"powershell ignored parameter", ".ps1", "function Write-Info([string]$Message) { Write-Host 'Ready' }\nWrite-Info 'stub nope'", nil},
		{"printf assignment helper", ".sh", `remember() { printf -v message '%s' "$1"; }; remember 'stub nope'`, nil},
		{"shell adjacent operator", ".sh", `true&&echo 'stub nope'`, []exampleBlock{{commands: []example{{"stub nope", 1}}}}},
		{"shell redirect filename", ".sh", `echo 'ready' > 'stub nope'`, nil},
		{"shell helper does not forward", ".sh", `check() { echo 'ready'; test -n "$1"; }; check 'stub nope'`, nil},
		{"shell helper forwards first argument", ".sh", `info() { echo "$1"; }; info 'stub run' 'stub nope'`, []exampleBlock{{commands: []example{{"stub run", 1}}}}},
		{"console commands and output", ".md", "```console\n$ stub run file\nstub nope\n$ stub group child ID \\\n  --bogus\nstub started\n```", []exampleBlock{{commands: []example{{"stub run file", 2}, {"stub group child ID  --bogus", 4}}}}},
		{"shell static branches", ".sh", `if x; then echo 'stub run'; else echo 'stub nope'; fi`, []exampleBlock{{commands: []example{{"stub run", 1}, {"stub nope", 1}}}}},
		{"shell case arm", ".sh", "case x in\n  *) echo 'stub nope' ;;\nesac", []exampleBlock{{commands: []example{{"stub nope", 2}}}}},
		{"printf unquoted format", ".sh", `printf %s 'stub plain \n--bogus'`, []exampleBlock{{commands: []example{{`stub plain \n--bogus`, 1}}}}},
		{"powershell tags", ".ps1", `Write-Information 'Ready' -Tags 'stub nope'`, nil},
		{"shell heredoc", ".sh", "cat > x <<EOF\necho 'stub nope'\ndon't\nEOF\necho 'stub run'", []exampleBlock{{commands: []example{{"stub run", 5}}}}},
		{"version output beside command", ".md", "```text\nstub v0.38.0 (commit abc)\nstub nope\n```", []exampleBlock{{commands: []example{{"stub nope", 3}}}}},
		{"shell direct output", ".sh", "# installer\necho 'stub nope'", []exampleBlock{{commands: []example{{"stub nope", 2}}}}},
		{"printf argument stays literal", ".sh", `printf '%s\n' 'stub run C:\new'`, []exampleBlock{{commands: []example{{`stub run C:\new`, 1}}}}},
		{"uncalled helper", ".sh", "unused() {\necho 'stub nope'\n}\necho 'stub run'", []exampleBlock{{commands: []example{{"stub nope", 2}, {"stub run", 4}}}}},
		{"shell non-output strings", ".sh", `grep 'stub nope' file; command='stub nope' # echo 'stub nope'`, nil},
		{"powershell non-output strings", ".ps1", "Select-String 'stub nope' file\n$command = 'stub nope'", nil},
		{"powershell computed output", ".ps1", "Write-Host (Get-Thing 'stub nope')", nil},
		{"powershell direct output", ".ps1", "# installer\nWrite-Host 'stub nope'", []exampleBlock{{commands: []example{{"stub nope", 2}}}}},
		{"powershell uncalled helper", ".ps1", "function unused {\nWrite-Host 'stub nope'\n}\nWrite-Host 'stub run'", []exampleBlock{{commands: []example{{"stub nope", 2}, {"stub run", 4}}}}},
		{"powershell printer wrapper", ".ps1", "function info([string]$Message) { Write-Host $Message -ForegroundColor Green }\ninfo 'Get started: stub nope'", []exampleBlock{{commands: []example{{"stub nope", 2}}}}},
		{"installer expected-failure marker", ".ps1", "# release-commands: expected-failure\nWrite-Host 'stub run file'", []exampleBlock{{marker: "expected-failure marker", commands: []example{{"stub run file", 2}}}}},
		{"colon argument", ".sh", `echo 'stub run host:stub target'`, []exampleBlock{{commands: []example{{"stub run host:stub target", 1}}}}},
		{"printf assignment", ".sh", `printf -v x 'stub nope'`, nil},
		{"powershell comments", ".ps1", "# stub nope\n<# stub nope #>\nWrite-Host 'ready' # stub nope", nil},
		{"table", ".md", "| Example |\n| --- |\n| `stub nope` |", []exampleBlock{{commands: []example{{"stub nope", 3}}}}},
		{"multiline echo", ".sh", "echo 'Next:\nstub nope'", nil},
		{"continuation", ".md", "```sh\nstub group child ID \\\n --bogus\n```", []exampleBlock{{commands: []example{{"stub group child ID  --bogus", 2}}}}},
		{"synopsis continuation", ".md", "```sh\nstub group child ID\n    [--bogus]\n```", []exampleBlock{{commands: []example{{"stub group child ID [--bogus]", 2}}}}},
		{"comment backslash", ".md", "```sh\nstub run file # note \\\nstub nope\n```", []exampleBlock{{commands: []example{{"stub run file # note " + "\\", 2}, {"stub nope", 3}}}}},
		{"literal backslashes", ".md", "```sh\nstub run file \\\\\nstub nope\n```", []exampleBlock{{commands: []example{{`stub run file \\`, 2}, {"stub nope", 3}}}}},
		{"html consumes unreleased note", ".md", "Not in the latest release yet\n\n<div>Spacer</div>\n\n`stub nope`", []exampleBlock{{commands: []example{{"stub nope", 5}}}}},
		{"nested marker", ".md", "> Not in the latest release yet\n>\n> ```sh\n> stub nope\n> ```", []exampleBlock{{marker: "unreleased note", commands: []example{{"stub nope", 4}}}}},
		{"quoted newline", ".md", "```sh\nstub run 'hello\n  world'\n```", []exampleBlock{{commands: []example{{"stub run 'hello\n  world'", 2}}}}},
		{"expected failure comment", ".md", "<!-- release-commands: expected-failure -->\n\n`stub run file`", []exampleBlock{{marker: "expected-failure marker", commands: []example{{"stub run file", 3}}}}},
		{"unreleased note", ".md", "Not in the latest release yet\n\n`stub nope`", []exampleBlock{{marker: "unreleased note", commands: []example{{"stub nope", 3}}}}},
		{"mdx inline", ".mdx", "Use `stub nope`.", []exampleBlock{{commands: []example{{"stub nope", 1}}}}},
		{"markdown inline", ".markdown", "Use `stub nope`.", []exampleBlock{{commands: []example{{"stub nope", 1}}}}},
		{"powershell same line", ".ps1", "if ($true) { Write-Host 'stub nope' }; Write-Host 'stub run'", []exampleBlock{{commands: []example{{"stub nope", 1}, {"stub run", 1}}}}},
		{"interpolated shell message", ".sh", `echo "stub nope $name"`, nil},
		{"interpolated powershell message", ".ps1", `Write-Host "stub nope $name"`, nil},
		{"shell marker blank line", ".sh", "# Not in the latest release yet\necho 'stub future'\n\necho 'stub run'", []exampleBlock{{marker: "unreleased note", commands: []example{{"stub future", 2}}}, {commands: []example{{"stub run", 4}}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			blocks, err := examples("example"+test.ext, []byte(test.source), []string{"stub"})
			require.NoError(t, err)
			require.Equal(t, test.want, blocks)
		})
	}
}
