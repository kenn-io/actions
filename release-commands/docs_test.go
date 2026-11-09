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
		{"shell conditional output", ".sh", `true && echo 'stub nope'`, []exampleBlock{{commands: []example{{"stub nope", 1}}}}},
		{"shell inline else", ".sh", `if x; then echo 'ready'; else echo 'stub nope'; fi`, []exampleBlock{{commands: []example{{"stub nope", 1}}}}},
		{"shell conditional assignment", ".sh", `echo 'ready' && msg='stub nope'`, nil},
		{"shell case arm", ".sh", "case x in\n  *) echo 'stub nope' ;;\nesac", []exampleBlock{{commands: []example{{"stub nope", 2}}}}},
		{"printf unquoted format", ".sh", `printf %s 'stub plain \n--bogus'`, []exampleBlock{{commands: []example{{`stub plain \n--bogus`, 1}}}}},
		{"printf double quoted newline", ".sh", `printf "Next:\\n  stub nope"`, []exampleBlock{{commands: []example{{"stub nope", 1}}}}},
		{"powershell tags", ".ps1", `Write-Information 'Ready' -Tags 'stub nope'`, nil},
		{"powershell unquoted metadata", ".ps1", `Write-Host -ForegroundColor Green 'stub nope'`, []exampleBlock{{commands: []example{{"stub nope", 1}}}}},
		{"powershell switch", ".ps1", `Write-Host -NoNewline 'stub nope'`, []exampleBlock{{commands: []example{{"stub nope", 1}}}}},
		{"shell heredoc", ".sh", "cat > x <<EOF\necho 'stub nope'\ndon't\nEOF\necho 'stub run'", []exampleBlock{{commands: []example{{"stub run", 5}}}}},
		{"shell escaped dollar", ".sh", `echo "stub plain \$path --bogus"`, []exampleBlock{{commands: []example{{"stub plain $path --bogus", 1}}}}},
		{"powershell escaped dollar", ".ps1", "Write-Host \"stub plain `$path --bogus\"", []exampleBlock{{commands: []example{{"stub plain $path --bogus", 1}}}}},
		{"version output beside command", ".md", "```text\nstub v0.38.0 (commit abc)\nstub nope\n```", []exampleBlock{{commands: []example{{"stub v0.38.0 (commit abc)", 2}, {"stub nope", 3}}}}},
		{"shell direct output", ".sh", "# installer\necho 'stub nope'", []exampleBlock{{commands: []example{{"stub nope", 2}}}}},
		{"shell inline then", ".sh", "if true; then echo 'stub nope'; fi", []exampleBlock{{commands: []example{{"stub nope", 1}}}}},
		{"nonprinting helper", ".sh", `check() { test -f "$1"; }; check 'stub nope'`, nil},
		{"shell printf literal", ".sh", `printf 'Get started: stub nope'`, []exampleBlock{{commands: []example{{"stub nope", 1}}}}},
		{"printf escaped lines", ".sh", "# installer\nprintf 'Next:\\n\\tstub nope\\n'", []exampleBlock{{commands: []example{{"stub nope", 2}}}}},
		{"printf hex ANSI", ".sh", `printf '\x1b[32mstub nope\x1b[0m\n'`, []exampleBlock{{commands: []example{{"stub nope", 1}}}}},
		{"printf colored label", ".sh", `printf '\033[1mGet started:\033[0m stub nope\n'`, []exampleBlock{{commands: []example{{"stub nope", 1}}}}},
		{"powershell ANSI", ".ps1", "Write-Host \"`e[32mstub nope`e[0m\"", []exampleBlock{{commands: []example{{"stub nope", 1}}}}},
		{"powershell color prefix", ".ps1", `Write-Host "${GREEN}stub nope"`, []exampleBlock{{commands: []example{{"stub nope", 1}}}}},
		{"printf argument stays literal", ".sh", `printf '%s\n' 'stub run C:\new'`, []exampleBlock{{commands: []example{{`stub run C:\new`, 1}}}}},
		{"echo escaped newline", ".sh", `echo -e "\n  stub nope"`, []exampleBlock{{commands: []example{{"stub nope", 1}}}}},
		{"powershell escaped newline", ".ps1", "Write-Host \"`n  stub nope\"", []exampleBlock{{commands: []example{{"stub nope", 1}}}}},
		{"uncalled helper", ".sh", "unused() {\necho 'stub nope'\n}\necho 'stub run'", []exampleBlock{{commands: []example{{"stub nope", 2}, {"stub run", 4}}}}},
		{"called helper", ".sh", "show() {\necho 'stub nope'\n}\nmain() { show; }\nmain", []exampleBlock{{commands: []example{{"stub nope", 2}}}}},
		{"shell non-output strings", ".sh", `grep 'stub nope' file; command='stub nope' # echo 'stub nope'`, nil},
		{"powershell non-output strings", ".ps1", "Select-String 'stub nope' file\n$command = 'stub nope'", nil},
		{"powershell multiline non-output string", ".ps1", "$message = \"\nWrite-Host 'stub nope'\n\"\nWrite-Host 'stub run'", []exampleBlock{{commands: []example{{"stub run", 4}}}}},
		{"powershell computed output", ".ps1", "Write-Host (Get-Thing 'stub nope')", nil},
		{"powershell direct output", ".ps1", "# installer\nWrite-Host 'stub nope'", []exampleBlock{{commands: []example{{"stub nope", 2}}}}},
		{"powershell uncalled helper", ".ps1", "function unused {\nWrite-Host 'stub nope'\n}\nWrite-Host 'stub run'", []exampleBlock{{commands: []example{{"stub nope", 2}, {"stub run", 4}}}}},
		{"powershell called helper", ".ps1", "function show {\nWrite-Host 'stub nope'\n}\nshow", []exampleBlock{{commands: []example{{"stub nope", 2}}}}},
		{"powershell uncalled wrapper", ".ps1", "function info($Message) { Write-Host $Message }\nfunction unused {\ninfo 'stub nope'\n}\ninfo 'stub run'", []exampleBlock{{commands: []example{{"stub nope", 3}, {"stub run", 5}}}}},
		{"powershell printer wrapper", ".ps1", "function info {\nparam($Message)\nWrite-Host $Message\n}\ninfo 'Get started: stub nope'", []exampleBlock{{commands: []example{{"stub nope", 5}}}}},
		{"shell color prefix", ".sh", `echo "${GREEN}stub nope"`, []exampleBlock{{commands: []example{{"stub nope", 1}}}}},
		{"shell ANSI output", ".sh", "echo '\x1b[32mstub nope\x1b[0m'", []exampleBlock{{commands: []example{{"stub nope", 1}}}}},
		{"powershell marker", ".ps1", "# Not in the latest release yet\nWrite-Host 'stub nope'", []exampleBlock{{marker: "unreleased note", commands: []example{{"stub nope", 2}}}}},
		{"installer prose", ".sh", "echo 'stub started (pid 12)'", nil},
		{"quoted assignment parse failure", ".md", "`MODE=\"a b\" stub run (`", []exampleBlock{{commands: []example{{`MODE="a b" stub run (`, 1}}}}},
		{"colon argument", ".sh", `echo 'stub run host:stub target'`, []exampleBlock{{commands: []example{{"stub run host:stub target", 1}}}}},
		{"printf assignment", ".sh", `printf -v x 'stub nope'`, nil},
		{"powershell prefix", ".ps1", `Write-Host 'Get started: stub nope'`, []exampleBlock{{commands: []example{{"stub nope", 1}}}}},
		{"powershell comments", ".ps1", "# stub nope\n<# stub nope #>\nWrite-Host 'ready' # stub nope", nil},
		{"table", ".md", "| Example |\n| --- |\n| `stub nope` |", []exampleBlock{{commands: []example{{"stub nope", 3}}}}},
		{"helper default field separator", ".sh", `info() { echo $1; }; info 'stub run FILE'`, []exampleBlock{{commands: []example{{"stub run FILE", 1}}}}},
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
		{"helper continuation unsupported", ".sh", "info() { echo \"$1\"; }\ninfo \\\n'stub nope'", nil},
		{
			name:   "nested containers keep marker",
			ext:    ".sh",
			source: "info() { { echo \"$1\"; }; }\nmain() {\n# Not in the latest release yet\ninfo 'stub future'\n}\nmain",
			want:   []exampleBlock{{marker: "unreleased note", commands: []example{{"stub future", 4}}}},
		},
		{"mdx inline", ".mdx", "Use `stub nope`.", []exampleBlock{{commands: []example{{"stub nope", 1}}}}},
		{"markdown inline", ".markdown", "Use `stub nope`.", []exampleBlock{{commands: []example{{"stub nope", 1}}}}},
		{"wrapper forward declaration", ".sh", "info() { output \"$1\"; }\noutput() { echo \"$1\"; }\ninfo 'stub nope'", []exampleBlock{{commands: []example{{"stub nope", 3}}}}},
		{"wrapper reverse declaration", ".sh", "output() { echo \"$1\"; }\ninfo() { output \"$1\"; }\ninfo 'stub nope'", []exampleBlock{{commands: []example{{"stub nope", 3}}}}},
		{"powershell nested unused helper", ".ps1", "function unused {\nif ($true) { Write-Host 'ready' }\nWrite-Host 'stub nope'\n}", []exampleBlock{{commands: []example{{"stub nope", 3}}}}},
		{"powershell same line", ".ps1", "if ($true) { Write-Host 'stub nope' }; Write-Host 'stub run'", []exampleBlock{{commands: []example{{"stub nope", 1}, {"stub run", 1}}}}},
		{"quoted output decoy", ".sh", `text="echo 'stub nope'"; grep 'stub nope' file`, nil},
		{"commented helper declaration", ".sh", "# info() { echo \"$1\"; }\ninfo 'stub nope'", nil},
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
