package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExamples(t *testing.T) {
	for _, test := range []struct {
		name, ext, source, marker string
		want                      []example
	}{
		{"shell function marker", ".sh", "info() {\n# Not in the latest release yet\necho \"$1\"\n}\ninfo 'stub nope'", "unreleased note", []example{{"stub nope", 5}}},
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

func TestInstallerMarkers(t *testing.T) {
	for _, test := range []struct {
		name, source string
		want         []exampleBlock
	}{
		{
			name: "helper continuation ends at blank line",
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
			name:   "nested blank line",
			source: "info() { echo \"$1\"; }\nmain() {\n# Not in the latest release yet\ninfo 'stub future'\n\ninfo 'stub nope'\n}\nmain",
			want: []exampleBlock{
				{marker: "unreleased note", commands: []example{{"stub future", 4}}},
				{commands: []example{{"stub nope", 6}}},
			},
		},
		{
			name:   "nested containers keep marker",
			source: "info() { { echo \"$1\"; }; }\nmain() {\n# Not in the latest release yet\ninfo 'stub future'\n}\nmain",
			want:   []exampleBlock{{marker: "unreleased note", commands: []example{{"stub future", 4}}}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			blocks, err := examples("example.sh", []byte(test.source), []string{"stub"})
			require.NoError(t, err)
			require.Equal(t, test.want, blocks)
		})
	}
}
