package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/spf13/cobra" // The fixture CLI uses Cobra from this module.
	"github.com/stretchr/testify/require"
)

var fixture string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "release-fixture-*")
	if err != nil {
		panic(err)
	}
	binary := filepath.Join(dir, "stub")
	cmd := exec.CommandContext(context.Background(), "go", "build", "-o", binary, "./testdata/stub")
	if output, err := cmd.CombinedOutput(); err != nil {
		panic(fmt.Sprintf("%s: %v", output, err))
	}
	fixture = filepath.Join(dir, "stub_1.0.0_linux_amd64.tar.gz")
	data, err := os.ReadFile(binary)
	if err != nil {
		panic(err)
	}
	if err := writeFixtureArchive(fixture, map[string][]byte{"bin/stub": data}); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func writeFixtureArchive(path string, contents map[string][]byte) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	compressed := gzip.NewWriter(file)
	defer compressed.Close()
	archive := tar.NewWriter(compressed)
	defer archive.Close()
	for name, data := range contents {
		if err := archive.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(data))}); err != nil {
			return err
		}
		if _, err := archive.Write(data); err != nil {
			return err
		}
	}
	return errors.Join(archive.Close(), compressed.Close(), file.Close())
}

func TestReleaseCommands(t *testing.T) {
	for _, test := range []struct {
		name, ext, content, finding string
		line                        int
	}{
		{"disabled flag parsing", ".md", "`stub unsafe file --help=false`", "", 0},
		{"unknown command", ".md", "Intro\n\n`stub nope`", `v1.0.0 has no command "nope"`, 3},
		{"absent child", ".md", "```sh\nstub group absent\n```", `has no command "absent"`, 2},
		{"unknown flag", ".md", "`stub run file --bogus`", `has no flag "--bogus"`, 1},
		{"hidden flag", ".md", "`stub run file --hidden`", `has no flag "--hidden"`, 1},
		{"parent flag", ".md", "`stub group --local child ID`", `has no flag "--local"`, 1},
		{"inherited flag", ".md", "`stub --config cfg group child ID`", "", 0},
		{"missing binary", ".md", "`stub-helper run`", "stub-helper: not in", 1},
		{"inline argument", ".md", "Use `stub run ~/Documents`.", "", 0},
		{"alias", ".md", "`stub group alias <id> --limit <n>`", "", 0},
		{"long assignment", ".md", "`stub group child ID --limit=anything`", "", 0},
		{"short attached value", ".md", "`stub group child ID -n50`", "", 0},
		{"short separated value", ".md", "`stub group child ID -n <n>`", "", 0},
		{"positional hyphen", ".md", "`stub run -`", "", 0},
		{"terminator", ".md", "`stub run -- --bogus`", "", 0},
		{"table", ".md", "| Example |\n| --- |\n| `stub nope` |", `has no command "nope"`, 3},
		{"shell prefix", ".sh", `echo 'Get started: stub nope'`, `has no command "nope"`, 1},
		{"shell joined args", ".sh", `echo 'stub run' 'file --bogus'`, `has no flag "--bogus"`, 1},
		{"shell wrapper", ".sh", `info() { echo "$*"; }; info 'Get started: stub nope'`, `has no command "nope"`, 1},
		{"printf prefix", ".sh", `printf '%s\n' 'Get started: stub nope'`, `has no command "nope"`, 1},
		{"powershell prefix", ".ps1", `Write-Host 'Get started: stub nope'`, `has no command "nope"`, 1},
		{"powershell wrapper", ".ps1", `function Write-Info($message) { Write-Host $message }; Write-Info 'stub nope'`, `has no command "nope"`, 1},
		{"optional flag", ".md", "`stub group child <id> [--bogus]`", `has no flag "--bogus"`, 1},
		{"alternative", ".md", "`stub group child|alias <id>`", "", 0},
		{"absent alternative", ".md", "`stub group child|nope <id>`", `has no command "nope"`, 1},
		{"continuation", ".md", "```sh\nstub group child ID \\\n --bogus\n```", `has no flag "--bogus"`, 2},
		{"synopsis continuation", ".md", "```sh\nstub group child ID\n    [--bogus]\n```", `has no flag "--bogus"`, 2},
		{"shell operators", ".md", "`stub run file | cat && stub nope`", `has no command "nope"`, 1},
		{"parse failure", ".md", "`stub run (`", "encountered", 1},
		{"unreleased", ".md", "Not in the latest release yet\n\n`stub nope`", "", 0},
		{"expected failure", ".md", "<!-- release-commands: expected-failure -->\n\n`stub nope`", "", 0},
		{"nested marker", ".md", "> Not in the latest release yet\n>\n> ```sh\n> stub nope\n> ```", "", 0},
		{"passing marker", ".md", "Not in the latest release yet\n\n`stub run file`", "passes; remove unreleased note", 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "example"+test.ext)
			require.NoError(t, os.WriteFile(file, []byte(test.content), 0o600))
			var out, errOut bytes.Buffer
			code := run(t.Context(), []string{"--archive", fixture, "--release", "v1.0.0", "--binary", "stub", "--binary", "stub-helper", file}, &out, &errOut)
			if strings.HasPrefix(test.finding, "passes;") {
				require.Equal(t, 0, code, errOut.String())
				require.Contains(t, out.String(), test.finding)
			} else if test.finding != "" {
				require.Equal(t, 1, code, errOut.String())
				require.Contains(t, errOut.String(), test.finding)
			} else {
				require.Equal(t, 0, code, errOut.String())
				require.Empty(t, errOut.String())
				require.Empty(t, out.String())
			}
			if test.line > 0 {
				require.Contains(t, out.String()+errOut.String(), fmt.Sprintf("%s:%d:", file, test.line))
			}
		})
	}
}
