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
		{"shell function marker", ".sh", "info() {\n# Not in the latest release yet\necho \"$1\"\n}\ninfo 'stub nope'", "unreleased note", []example{{"stub nope", 5}}},
		{"powershell function marker", ".ps1", "function info($message) {\n# release-commands: expected-failure\nWrite-Host $message\n}\ninfo 'stub nope'", "expected-failure marker", []example{{"stub nope", 5}}},
		{"powershell prefix", ".ps1", `Write-Host 'Get started: stub nope'`, "", []example{{"stub nope", 1}}},
		{"powershell helper argument", ".ps1", "function Write-Info([string]$Message) { Write-Host $Message -ForegroundColor Green }\nWrite-Info `\n'stub nope'", "", []example{{"stub nope", 3}}},
		{"powershell named helper", ".ps1", "function info { param($first, $second) Write-Host ($first + ' ' + $second) }\ninfo -second 'nope' 'stub'", "", []example{{"stub nope", 2}}},
		{"powershell uncalled helper", ".ps1", `function info { Write-Host 'stub nope' }`, "", nil},
		{"powershell array separator", ".ps1", `Write-Host @('stub run file', 'bogus') -Separator ' --'`, "", []example{{"stub run file --bogus", 1}}},
		{"powershell separate output", ".ps1", "Write-Output 'stub run',\n'stub nope'", "", []example{{"stub run", 1}, {"stub nope", 2}}},
		{"powershell no newline", ".ps1", "Write-Host 'stub ' -NoNewline\nWrite-Host 'nope'", "", []example{{"stub nope", 1}}},
		{"powershell information tags", ".ps1", "Write-Information 'Ready' 'stub run' -InformationAction Continue\nWrite-Information -Tags 'stub run' -MessageData 'stub nope'", "", []example{{"stub nope", 2}}},
		{"powershell multiline string", ".ps1", "Write-Host 'Next:\nstub nope'", "", []example{{"stub nope", 2}}},
		{"powershell here-string", ".ps1", "Write-Host @'\nstub nope\n'@", "", []example{{"stub nope", 2}}},
		{"powershell comments", ".ps1", "# stub nope\n<# stub nope #>\nWrite-Host 'ready' # stub nope", "", nil},
		{"powershell unevaluated expressions", ".ps1", "Write-Host \"Next: $(throw 'evaluated')stub nope\"\nWrite-Host (Get-Thing 'stub run')", "", []example{{"stub nope", 1}}},
		{"colored printf", ".sh", `printf '\033[32mstub nope\033[0m\n'`, "", []example{{"stub nope", 1}}},
		{"colored echo", ".sh", `echo -e '\033[32mstub nope\033[0m'`, "", []example{{"stub nope", 1}}},
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
		{"continuation", ".md", "```sh\nstub group child ID \\\n --bogus\n```", "", []example{{"stub group child ID  --bogus", 2}}},
		{"synopsis continuation", ".md", "```sh\nstub group child ID\n    [--bogus]\n```", "", []example{{"stub group child ID [--bogus]", 2}}},
		{"comment backslash", ".md", "```sh\nstub run file # note \\\nstub nope\n```", "", []example{{"stub run file # note " + "\\", 2}, {"stub nope", 3}}},
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

func TestPowerShellUnavailable(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := examples("install.ps1", []byte("Write-Host 'stub nope'"), []string{"stub"})
	require.ErrorIs(t, err, exec.ErrNotFound)
}

func TestInstallerMarkers(t *testing.T) {
	for _, test := range []struct {
		name, ext, source string
		want              []exampleBlock
	}{
		{
			name: "helper continuation ends at blank line",
			ext:  ".sh",
			source: `info() { printf '%s\n' "$1"; }
# Not in the latest release yet
info \
'stub future'

echo 'stub run'`,
			want: []exampleBlock{
				{marker: "unreleased note", commands: []example{{"stub future", 4}}},
				{commands: []example{{"stub run", 6}}},
			},
		},
		{
			name: "powershell helper continuation ends at blank line",
			ext:  ".ps1",
			source: `function info($message) { Write-Output $message }
# Not in the latest release yet
info ` + "`" + `
'stub future'

Write-Host 'stub run'`,
			want: []exampleBlock{
				{marker: "unreleased note", commands: []example{{"stub future", 4}}},
				{commands: []example{{"stub run", 6}}},
			},
		},
		{
			name:   "nested blank line",
			ext:    ".sh",
			source: "info() { echo \"$1\"; }\nmain() {\n# Not in the latest release yet\ninfo 'stub future'\n\ninfo 'stub nope'\n}\nmain",
			want: []exampleBlock{
				{marker: "unreleased note", commands: []example{{"stub future", 4}}},
				{commands: []example{{"stub nope", 6}}},
			},
		},
		{
			name:   "nested containers keep marker",
			ext:    ".sh",
			source: "info() { { echo \"$1\"; }; }\nmain() {\n# Not in the latest release yet\ninfo 'stub future'\n}\nmain",
			want:   []exampleBlock{{marker: "unreleased note", commands: []example{{"stub future", 4}}}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			requirePowerShell(t, test.ext)
			blocks, err := examples("example"+test.ext, []byte(test.source), []string{"stub"})
			require.NoError(t, err)
			require.Equal(t, test.want, blocks)
		})
	}
}
