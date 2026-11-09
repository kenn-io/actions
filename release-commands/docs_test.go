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
		{"alert unreleased note", ".md", "> [!NOTE]\n> Not in the latest release yet.\n\n`stub nope`", []expectedBlock{{marker: "unreleased note", commands: []expectedExample{{"stub nope", 4}}}}},
		{"joined echo operands", ".sh", `echo 'stub plain' 'stub nope'`, []expectedBlock{{commands: []expectedExample{{"stub plain stub nope", 1}}}}},
		{"printf helper prefixed message", ".sh", `info() { printf '%s\n' "$1"; }; info 'Get started: stub nope'`, []expectedBlock{{commands: []expectedExample{{"stub nope", 1}}}}},
		{"printf unused operand", ".sh", `printf 'stub plain\n' 'stub nonexistent'`, []expectedBlock{{commands: []expectedExample{{"stub plain", 1}}}}},
		{"printf helper unused operand", ".sh", `info() { printf 'stub run\n' "$1"; }; info 'stub nope'`, nil},
		{"printf substituted format", ".sh", `printf 'stub %s\n' plain`, []expectedBlock{{commands: []expectedExample{{"stub plain", 1}}}}},
		{"shell colored helper", ".sh", `info() { printf "${GREEN}%s${NC}\n" "$1"; }; info 'stub nope'`, nil},
		{"shell static branches", ".sh", `if x; then echo 'stub run'; else echo 'stub nope'; fi`, []expectedBlock{{commands: []expectedExample{{"stub run", 1}, {"stub nope", 1}}}}},
		{"version output beside command", ".md", "```text\nstub v0.38.0 (commit abc)\nstub nope\n```", []expectedBlock{{commands: []expectedExample{{"stub nope", 3}}}}},
		{"installer function", ".sh", "install() {\necho 'stub nope'\n}\ninstall", []expectedBlock{{commands: []expectedExample{{"stub nope", 2}}}}},
		{"shell non-output strings", ".sh", `grep 'stub nope' file; command='stub nope' # echo 'stub nope'`, nil},
		{"powershell non-output strings", ".ps1", "Select-String 'stub nope' file\n$command = 'stub nope'", nil},
		{"powershell Write-Info call", ".ps1", "Write-Info 'Get started: stub nope && stub plain'", []expectedBlock{{commands: []expectedExample{{"stub nope && stub plain", 1}}}}},
		{"colon argument", ".sh", `echo 'stub run host:stub target'`, []expectedBlock{{commands: []expectedExample{{"stub run host:stub target", 1}}}}},
		{"table", ".md", "| Example |\n| --- |\n| `stub get\\|show` |", []expectedBlock{{commands: []expectedExample{{"stub get|show", 3}}}}},
		{"shell prompt", ".md", "```sh\n$ stub group child ID \\\n --bogus\n```", []expectedBlock{{commands: []expectedExample{{"stub group child ID  --bogus", 2}}}}},
		{"continuation", ".md", "```sh\nstub group child ID \\\n --bogus\n```", []expectedBlock{{commands: []expectedExample{{"stub group child ID  --bogus", 2}}}}},
		{"synopsis continuation", ".md", "```sh\nstub group child ID\n    [--bogus]\n```", []expectedBlock{{commands: []expectedExample{{"stub group child ID [--bogus]", 2}}}}},
		{"html consumes unreleased note", ".md", "Not in the latest release yet\n\n<div>Spacer</div>\n\n`stub nope`", []expectedBlock{{commands: []expectedExample{{"stub nope", 5}}}}},
		{"nested marker", ".md", "> Not in the latest release yet\n>\n> ```sh\n> stub nope\n> ```", []expectedBlock{{marker: "unreleased note", commands: []expectedExample{{"stub nope", 4}}}}},
		{"expected failure comment", ".md", "<!-- release-commands: expected-failure -->\n\n`stub run file`", []expectedBlock{{marker: "expected-failure marker", commands: []expectedExample{{"stub run file", 3}}}}},
		{"unreleased note", ".md", "Not in the latest release yet\n\n`stub nope`", []expectedBlock{{marker: "unreleased note", commands: []expectedExample{{"stub nope", 3}}}}},
		{"note paragraph command", ".md", "Not in the latest release yet: `stub run file`", []expectedBlock{{marker: "unreleased note", commands: []expectedExample{{"stub run file", 1}}}}},
		{"interpolated shell message", ".sh", `echo "stub nope $name"`, nil},
		{"powershell double quoted message", ".ps1", `Write-Host "  stub nope"`, []expectedBlock{{commands: []expectedExample{{"stub nope", 1}}}}},
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

func TestExampleParseError(t *testing.T) {
	_, err := examples("example.sh", []byte("echo 'stub nope'\nfoo() { case"), []string{"stub"})
	require.ErrorContains(t, err, "example.sh:2:")
}
