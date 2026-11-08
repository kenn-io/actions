package main

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExamples(t *testing.T) {
	for _, test := range []struct {
		name, ext, source, marker string
		want                      []example
	}{
		{"formatted wrapper output", ".sh", `info() { printf '%s\n' "$*"; }; info 'stub run' 'file --bogus'`, "", []example{{"stub run file --bogus", 1}}},
		{"table", ".md", "| Example |\n| --- |\n| `stub nope` |", "", []example{{"stub nope", 3}}},
		{"shell joined args", ".sh", `echo 'stub run' 'file --bogus'`, "", []example{{"stub run file --bogus", 1}}},
		{"echo escape option", ".sh", "# installer\necho -E -ne 'Next:\\n  stub run file --bogus'", "", []example{{"stub run file --bogus", 2}}},
		{"echo final literal option", ".sh", `echo -e -nE 'Next:\n  stub run file --bogus'`, "", nil},
		{"echo helper escape", ".sh", "info() { echo -E -ne \"$1\"; }\ninfo 'Next:\\n  stub run file --bogus'", "", []example{{"stub run file --bogus", 2}}},
		{"echo helper final literal", ".sh", `info() { echo -e -nE "$*"; }; info 'Next:\n  stub run file --bogus'`, "", nil},
		{"powershell prefix", ".ps1", `Write-Host 'Get started: stub nope'`, "", []example{{"stub nope", 1}}},
		{"powershell joined arguments", ".ps1", `Write-Host 'stub run' 'file --bogus'`, "", []example{{"stub run file --bogus", 1}}},
		{"powershell foreground color", ".ps1", `Write-Host -ForegroundColor Green 'stub run file --bogus'`, "", []example{{"stub run file --bogus", 1}}},
		{"powershell separate output", ".ps1", `Write-Output 'stub run' 'stub nope'`, "", []example{{"stub run", 1}, {"stub nope", 1}}},
		{"powershell array output", ".ps1", "Write-Output @(\n'stub nope')", "", []example{{"stub nope", 2}}},
		{"powershell dynamic array", ".ps1", `Write-Output @((Get-Thing 'stub nope'))`, "", nil},
		{"powershell comma array output", ".ps1", "Write-Output 'stub run',\n'stub nope'", "", []example{{"stub run", 1}, {"stub nope", 2}}},
		{"powershell array separator", ".ps1", `Write-Host @('stub run file', 'bogus') -Separator ' --'`, "", []example{{"stub run file --bogus", 1}}},
		{"powershell empty separator", ".ps1", `Write-Host 'stub run file --bo' 'gus' -Separator ''`, "", []example{{"stub run file --bogus", 1}}},
		{"powershell wrapper", ".ps1", `function Write-Info($message) { Write-Host $message }; Write-Info 'stub nope'`, "", []example{{"stub nope", 1}}},
		{"continuation", ".md", "```sh\nstub group child ID \\\n --bogus\n```", "", []example{{"stub group child ID  --bogus", 2}}},
		{"synopsis continuation", ".md", "```sh\nstub group child ID\n    [--bogus]\n```", "", []example{{"stub group child ID [--bogus]", 2}}},
		{"literal backslashes", ".md", "```sh\nstub run file \\\\\nstub nope\n```", "", []example{{`stub run file \\`, 2}, {"stub nope", 3}}},
		{"parse failure", ".md", "`FOO=1 stub run (`", "", []example{{"FOO=1 stub run (", 1}}},
		{"html consumes unreleased note", ".md", "Not in the latest release yet\n\n<div>Spacer</div>\n\n`stub nope`", "", []example{{"stub nope", 5}}},
		{"nested marker", ".md", "> Not in the latest release yet\n>\n> ```sh\n> stub nope\n> ```", "unreleased note", []example{{"stub nope", 4}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.ext == ".ps1" {
				if _, err := exec.LookPath("pwsh"); err != nil {
					t.Skip("pwsh is required for PowerShell installers")
				}
			}
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
