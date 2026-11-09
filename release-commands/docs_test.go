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
		{"version output beside command", ".md", "```text\nstub v0.38.0 (commit abc)\nstub nope\n```", []exampleBlock{{commands: []example{{"stub v0.38.0 (commit abc)", 2}, {"stub nope", 3}}}}},
		{"shell direct output", ".sh", "# installer\necho 'stub nope'", []exampleBlock{{commands: []example{{"stub nope", 2}}}}},
		{"shell printf literal", ".sh", `printf 'Get started: stub nope'`, []exampleBlock{{commands: []example{{"stub nope", 1}}}}},
		{"shell non-output strings", ".sh", `grep 'stub nope' file; command='stub nope' # echo 'stub nope'`, nil},
		{"powershell non-output strings", ".ps1", "Select-String 'stub nope' file\n$command = 'stub nope'", nil},
		{"powershell multiline non-output string", ".ps1", "$message = \"\nWrite-Host 'stub nope'\n\"\nWrite-Host 'stub run'", []exampleBlock{{commands: []example{{"stub run", 4}}}}},
		{"powershell computed output", ".ps1", "Write-Host (Get-Thing 'stub nope')", nil},
		{"powershell direct output", ".ps1", "# installer\nWrite-Host 'stub nope'", []exampleBlock{{commands: []example{{"stub nope", 2}}}}},
		{"powershell printer wrapper", ".ps1", "function info {\nparam($Message)\nWrite-Host $Message\n}\ninfo 'Get started: stub nope'", []exampleBlock{{commands: []example{{"stub nope", 5}}}}},
		{"powershell Write-Info", ".ps1", "function Write-Info($Message) { Write-Host $Message }\nWrite-Info 'Get started: stub nope'", []exampleBlock{{commands: []example{{"stub nope", 2}}}}},
		{"shell color prefix", ".sh", `echo "${GREEN}stub nope"`, []exampleBlock{{commands: []example{{"stub nope", 1}}}}},
		{"shell ANSI output", ".sh", "echo '\x1b[32mstub nope\x1b[0m'", []exampleBlock{{commands: []example{{"stub nope", 1}}}}},
		{"powershell marker", ".ps1", "# Not in the latest release yet\nWrite-Host 'stub nope'", []exampleBlock{{marker: "unreleased note", commands: []example{{"stub nope", 2}}}}},
		{"installer prose", ".sh", "echo 'stub started (pid 12)'", nil},
		{"quoted assignment parse failure", ".md", "`MODE=\"a b\" stub run (`", []exampleBlock{{commands: []example{{`MODE="a b" stub run (`, 1}}}}},
		{"colon argument", ".sh", `echo 'stub run host:stub target'`, []exampleBlock{{commands: []example{{"stub run host:stub target", 1}}}}},
		{"printf assignment", ".sh", `printf -v message '%s' 'ready'; echo 'stub nope'`, []exampleBlock{{commands: []example{{"stub nope", 1}}}}},
		{"powershell prefix", ".ps1", `Write-Host 'Get started: stub nope'`, []exampleBlock{{commands: []example{{"stub nope", 1}}}}},
		{"powershell comments", ".ps1", "# stub nope\n<# stub nope #>\nWrite-Host 'ready' # stub nope", nil},
		{"table", ".md", "| Example |\n| --- |\n| `stub nope` |", []exampleBlock{{commands: []example{{"stub nope", 3}}}}},
		{"helper default field separator", ".sh", `info() { echo $1; }; info 'stub run FILE'`, []exampleBlock{{commands: []example{{"stub run FILE", 1}}}}},
		{"helper colored output", ".sh", `GREEN='\033[32m'; NC='\033[0m'; info() { echo -e "${GREEN}$1${NC}"; }; info 'stub nope'`, []exampleBlock{{commands: []example{{"stub nope", 1}}}}},
		{"multiline echo", ".sh", "echo 'Next:\nstub nope'", []exampleBlock{{commands: []example{{"stub nope", 2}}}}},
		{"continuation", ".md", "```sh\nstub group child ID \\\n --bogus\n```", []exampleBlock{{commands: []example{{"stub group child ID  --bogus", 2}}}}},
		{"synopsis continuation", ".md", "```sh\nstub group child ID\n    [--bogus]\n```", []exampleBlock{{commands: []example{{"stub group child ID [--bogus]", 2}}}}},
		{"comment backslash", ".md", "```sh\nstub run file # note \\\nstub nope\n```", []exampleBlock{{commands: []example{{"stub run file # note " + "\\", 2}, {"stub nope", 3}}}}},
		{"literal backslashes", ".md", "```sh\nstub run file \\\\\nstub nope\n```", []exampleBlock{{commands: []example{{`stub run file \\`, 2}, {"stub nope", 3}}}}},
		{"html consumes unreleased note", ".md", "Not in the latest release yet\n\n<div>Spacer</div>\n\n`stub nope`", []exampleBlock{{commands: []example{{"stub nope", 5}}}}},
		{"nested marker", ".md", "> Not in the latest release yet\n>\n> ```sh\n> stub nope\n> ```", []exampleBlock{{marker: "unreleased note", commands: []example{{"stub nope", 4}}}}},
		{"quoted newline", ".md", "```sh\nstub run 'hello\n  world'\n```", []exampleBlock{{commands: []example{{"stub run 'hello\n  world'", 2}}}}},
		{"expected failure comment", ".md", "<!-- release-commands: expected-failure -->\n\n`stub run file`", []exampleBlock{{marker: "expected-failure marker", commands: []example{{"stub run file", 3}}}}},
		{"unreleased note", ".md", "Not in the latest release yet\n\n`stub nope`", []exampleBlock{{marker: "unreleased note", commands: []example{{"stub nope", 3}}}}},
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
			name:   "nested containers keep marker",
			ext:    ".sh",
			source: "info() { { echo \"$1\"; }; }\nmain() {\n# Not in the latest release yet\ninfo 'stub future'\n}\nmain",
			want:   []exampleBlock{{marker: "unreleased note", commands: []example{{"stub future", 4}}}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			blocks, err := examples("example"+test.ext, []byte(test.source), []string{"stub"})
			require.NoError(t, err)
			require.Equal(t, test.want, blocks)
		})
	}
}
