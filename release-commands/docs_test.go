package main

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExamples(t *testing.T) {
	for _, test := range []struct {
		name, ext, source, marker string
		want                      []example
	}{
		{"shell function marker", ".sh", "info() {\n# Not in the latest release yet\necho \"$1\"\n}\ninfo 'stub nope'", "unreleased note", []example{{"stub nope", 5}}},
		{"powershell function marker", ".ps1", "function info($message) {\n# release-commands: expected-failure\nWrite-Host $message\n}\ninfo 'stub nope'", "expected-failure marker", []example{{"stub nope", 5}}},
		{"shell split output", ".sh", "printf 'stub '\nprintf '%s\\n' 'nope'", "", []example{{"stub nope", 1}}},
		{"powershell CRLF marker", ".ps1", "# release-commands: expected-failure\r\nWrite-Host 'stub nope'", "expected-failure marker", []example{{"stub nope", 2}}},
		{"colored printf", ".sh", `printf '\033[32mstub nope\033[0m\n'`, "", []example{{"stub nope", 1}}},
		{"colored echo", ".sh", `echo -e '\033[32mstub nope\033[0m'`, "", []example{{"stub nope", 1}}},
		{"colored powershell", ".ps1", "Write-Host \"`e[32mstub nope`e[0m\"", "", []example{{"stub nope", 1}}},
		{"powershell prefixed helper", ".ps1", "function info($message) { Write-Host ('Next: ' + $message) }\ninfo `\n'stub nope'", "", []example{{"stub nope", 3}}},
		{"powershell named helper", ".ps1", "function info { param($first, $second) Write-Host ($first + ' ' + $second) }\ninfo -first 'stub' 'nope'", "", []example{{"stub nope", 2}}},
		{"powershell nested helper array", ".ps1", "function outer($messages) { inner $messages }; function inner($messages) { Write-Output $messages }\nouter @('stub run', 'stub nope')", "", []example{{"stub run", 2}, {"stub nope", 2}}},
		{"powershell uncalled helper", ".ps1", `function info { Write-Host 'stub nope' }`, "", nil},
		{"powershell helper joined parameters", ".ps1", `function info($first, $second) { Write-Host "$first $second" }; info 'stub' 'nope'`, "", []example{{"stub nope", 1}}},
		{"powershell helper unused argument", ".ps1", `function info($first, $second) { Write-Host $first }; info 'ready' 'stub nope'`, "", nil},
		{"formatted wrapper output", ".sh", `info() { printf '%s\n' "$*"; }; info 'stub run' 'file --bogus'`, "", []example{{"stub run file --bogus", 1}}},
		{"table", ".md", "| Example |\n| --- |\n| `stub nope` |", "", []example{{"stub nope", 3}}},
		{"shell joined args", ".sh", `echo 'stub run' 'file --bogus'`, "", []example{{"stub run file --bogus", 1}}},
		{"helper all arguments", ".sh", `info() { echo "$@"; }; info 'stub' 'nope'`, "", []example{{"stub nope", 1}}},
		{"helper default field separator", ".sh", `info() { echo $1; }; info 'stub run FILE'`, "", []example{{"stub run FILE", 1}}},
		{"helper first argument", ".sh", `info() { echo "$1"; }; info 'ready' 'stub nope'`, "", nil},
		{"helper printf arguments", ".sh", `info() { printf "$@"; }; info '%s\n' 'stub nope'`, "", []example{{"stub nope", 1}}},
		{"helper colored output", ".sh", `GREEN='\033[32m'; NC='\033[0m'; info() { echo -e "${GREEN}$1${NC}"; }; info 'stub nope'`, "", []example{{"stub nope", 1}}},
		{"multiline echo", ".sh", "echo 'Next:\nstub nope'", "", []example{{"stub nope", 2}}},
		{"printf repeated multiline arguments", ".sh", "printf '%s\\n' 'stub nope' \\\n'stub nope'", "", []example{{"stub nope", 1}, {"stub nope", 2}}},
		{"helper multiline printf", ".sh", "info() { printf \"$@\"; }\ninfo '%s\\n' 'Next:\nstub nope'", "", []example{{"stub nope", 3}}},
		{"echo escape option", ".sh", "# installer\necho -E -ne 'Next:\\n  stub run file --bogus'", "", []example{{"stub run file --bogus", 2}}},
		{"echo final literal option", ".sh", `echo -e -nE 'Next:\n  stub run file --bogus'`, "", nil},
		{"powershell prefix", ".ps1", `Write-Host 'Get started: stub nope'`, "", []example{{"stub nope", 1}}},
		{"powershell joined arguments", ".ps1", `Write-Host 'stub run' 'file --bogus'`, "", []example{{"stub run file --bogus", 1}}},
		{"powershell foreground color", ".ps1", `Write-Host -ForegroundColor Green 'stub run file --bogus'`, "", []example{{"stub run file --bogus", 1}}},
		{"powershell separate output", ".ps1", `Write-Output 'stub run' 'stub nope'`, "", []example{{"stub run", 1}, {"stub nope", 1}}},
		{"powershell array output", ".ps1", "Write-Output @(\n'stub nope')", "", []example{{"stub nope", 2}}},
		{"powershell dynamic array", ".ps1", `Write-Output @((Get-Thing 'stub nope'))`, "", nil},
		{"powershell comma array output", ".ps1", "Write-Output 'stub run',\n'stub nope'", "", []example{{"stub run", 1}, {"stub nope", 2}}},
		{"powershell array separator", ".ps1", `Write-Host @('stub run file', 'bogus') -Separator ' --'`, "", []example{{"stub run file --bogus", 1}}},
		{"powershell empty separator", ".ps1", `Write-Host 'stub run file --bo' 'gus' -Separator ''`, "", []example{{"stub run file --bogus", 1}}},
		{"powershell static concatenation", ".ps1", `Write-Host ('stub ' + 'nope')`, "", []example{{"stub nope", 1}}},
		{"powershell dynamic concatenation", ".ps1", `Write-Host ('stub ' + $name)`, "", nil},
		{"powershell nested concatenation", ".ps1", `Write-Host ('stub ' + 'no' + 'pe')`, "", []example{{"stub nope", 1}}},
		{"powershell command in parens", ".ps1", `Write-Host (Get-Date)`, "", nil},
		{"continuation", ".md", "```sh\nstub group child ID \\\n --bogus\n```", "", []example{{"stub group child ID  --bogus", 2}}},
		{"synopsis continuation", ".md", "```sh\nstub group child ID\n    [--bogus]\n```", "", []example{{"stub group child ID [--bogus]", 2}}},
		{"literal backslashes", ".md", "```sh\nstub run file \\\\\nstub nope\n```", "", []example{{`stub run file \\`, 2}, {"stub nope", 3}}},
		{"parse failure", ".md", "`FOO=1 stub run (`", "", []example{{"FOO=1 stub run (", 1}}},
		{"html consumes unreleased note", ".md", "Not in the latest release yet\n\n<div>Spacer</div>\n\n`stub nope`", "", []example{{"stub nope", 5}}},
		{"nested marker", ".md", "> Not in the latest release yet\n>\n> ```sh\n> stub nope\n> ```", "unreleased note", []example{{"stub nope", 4}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			requirePowerShell(t, test.ext)
			blocks, err := examples("example"+test.ext, []byte(test.source), []string{"stub"})
			require.NoError(t, err)
			var got []example
			for _, block := range blocks {
				require.Equal(t, test.marker, block.marker)
				got = append(got, block.commands...)
			}
			require.Equal(t, test.want, got)
		})
	}
}

func requirePowerShell(t *testing.T, ext string) {
	t.Helper()
	if ext == ".ps1" {
		if _, err := exec.LookPath("pwsh"); err != nil {
			t.Skip("pwsh is required for PowerShell installers")
		}
	}
}

func TestInstallerMarkers(t *testing.T) {
	for _, test := range []struct{ ext, helper, call, printer, empty string }{
		{".sh", "info() { printf '%s\\n' \"$1\" \"$2\"; }", "info \\\n'stub nope' 'stub absent'", "echo", "echo\n"},
		{".ps1", "function info($first, $second) { Write-Output $first $second }", "info `\n'stub nope' 'stub absent'", "Write-Host", "Write-Host ''\n"},
	} {
		for _, ending := range []string{"\n", "ignored\n", "ignored; ", test.empty} {
			t.Run(test.ext+ending, func(t *testing.T) {
				requirePowerShell(t, test.ext)
				source := test.helper + "\n# Not in the latest release yet\n" + test.call + "\n" + test.printer + " 'stub future'\n" + ending + test.printer + " 'stub run'"
				blocks, err := examples("example"+test.ext, []byte(source), []string{"stub"})
				require.NoError(t, err)
				require.Equal(t, []exampleBlock{
					{marker: "unreleased note", commands: []example{{"stub nope", 4}, {"stub absent", 4}, {"stub future", 5}}},
					{commands: []example{{"stub run", 6 + strings.Count(ending, "\n")}}},
				}, blocks)
			})
		}
	}
}

func TestFunctionMarkers(t *testing.T) {
	for _, test := range []struct {
		name, ext, source string
		first, second     int
	}{
		{"shell empty output", ".sh", "main() {\n# Not in the latest release yet\necho 'stub future'\necho\necho 'stub nope'\n}\nmain", 3, 5},
		{"powershell empty output", ".ps1", "function main {\n# Not in the latest release yet\nWrite-Host 'stub future'\nWrite-Host ''\nWrite-Host 'stub nope'\n}\nmain", 3, 5},
		{"shell nested blank line", ".sh", "info() { echo \"$1\"; }\nmain() {\n# Not in the latest release yet\ninfo 'stub future'\n\ninfo 'stub nope'\n}\nmain", 4, 6},
		{"powershell nested blank line", ".ps1", "function info($message) { Write-Host $message }\nfunction main {\n# Not in the latest release yet\ninfo 'stub future'\n\ninfo 'stub nope'\n}\nmain", 4, 6},
		{"shell nested silent statement", ".sh", "info() { echo \"$1\"; }\nmain() {\n# Not in the latest release yet\ninfo 'stub future'\nignored\ninfo 'stub nope'\n}\nmain", 4, 6},
		{"powershell nested silent statement", ".ps1", "function info($message) { Write-Host $message }\nfunction main {\n# Not in the latest release yet\ninfo 'stub future'\nignored\ninfo 'stub nope'\n}\nmain", 4, 6},
		{"shell nested empty printf", ".sh", "info() { echo \"$1\"; }\nmain() {\n# Not in the latest release yet\ninfo 'stub future'\nprintf ''\ninfo 'stub nope'\n}\nmain", 4, 6},
	} {
		t.Run(test.name, func(t *testing.T) {
			requirePowerShell(t, test.ext)
			blocks, err := examples("example"+test.ext, []byte(test.source), []string{"stub"})
			require.NoError(t, err)
			require.Equal(t, []exampleBlock{
				{marker: "unreleased note", commands: []example{{"stub future", test.first}}},
				{commands: []example{{"stub nope", test.second}}},
			}, blocks)
		})
	}
}
