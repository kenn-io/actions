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
		{"markdown command substitution", ".md", "```sh\nTOKEN=$(stub nope)\n```", `has no command "nope"`, 2},
		{"markdown parse escape", ".md", "`stub started (pid 12)`", "add <!-- release-commands: expected-failure -->", 1},
		{"installer prose escape", ".sh", "# release-commands: expected-failure\necho 'stub is installed'", "", 0},
		{"installer unreleased", ".sh", "# Not in the latest release yet\necho 'stub future'", "", 0},
		{"installer passing marker", ".sh", "# release-commands: expected-failure\necho 'stub run file'", "passes; remove expected-failure marker", 2},
		{"disabled flag parsing", ".md", "`stub unsafe file --help=false`", "", 0},
		{"unknown command", ".md", "Intro\n\n`stub nope`", `v1.0.0 has no command "nope"`, 3},
		{"absent child", ".md", "```sh\nstub group absent\n```", `has no command "absent"`, 2},
		{"unknown flag", ".md", "`stub run file --bogus`", "unknown flag: --bogus", 1},
		{"child flag before child", ".md", "`stub group --limit 50 child ID`", "", 0},
		{"unknown flag before child", ".md", "`stub group --bogus child ID`", "unknown flag: --bogus", 1},
		{"missing child after deferred flag", ".md", "`stub --local=false group absent`", `has no command "absent"`, 1},
		{"unknown clustered shorthand", ".md", "`stub group child ID -hZ`", "unknown shorthand flag: 'Z'", 1},
		{"malformed shorthand", ".md", "`stub run file -=oops`", "unknown shorthand flag: '='", 1},
		{"parent flag", ".md", "`stub group --local child ID`", "unknown flag: --local", 1},
		{"inherited flag", ".md", "`stub --config cfg group child ID`", "", 0},
		{"misconfigured binary", ".md", "`stub nope`", "stbu: not in", 0},
		{"missing binary", ".md", "`stub-helper run`", "stub-helper: not in", 1},
		{"marked missing binary", ".md", "Not in the latest release yet\n\n`stub-helper run`", "", 0},
		{"leaf arguments", ".md", "`stub plain somefile`", "", 0},
		{"group argument synopsis", ".md", "`stub named child`", "", 0},
		{"long assignment", ".md", "`stub group --limit=anything child ID`", "", 0},
		{"short attached value", ".md", "`stub group -n50 child ID`", "", 0},
		{"terminator", ".md", "`stub group -- --bogus nope`", "", 0},
		{"printf prefix", ".sh", `printf '%s\n' 'Get started: stub nope'`, `has no command "nope"`, 1},
		{"unreleased", ".md", "Not in the latest release yet\n\n`stub nope`", "", 0},
		{"expected failure", ".md", "<!-- release-commands: expected-failure -->\n\n`stub nope`", "", 0},
		{"passing marker", ".md", "Not in the latest release yet\n\n`stub run file`", "passes; remove unreleased note", 3},
		{"passing expected failure", ".md", "<!-- release-commands: expected-failure -->\n\n`stub run file`", "passes; remove expected-failure marker", 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "example"+test.ext)
			require.NoError(t, os.WriteFile(file, []byte(test.content), 0o600))
			var out, errOut bytes.Buffer
			binary := "stub"
			if test.name == "misconfigured binary" {
				binary = "stbu"
			}
			args := []string{"--archive", fixture, "--release", "v1.0.0", "--binary", binary}
			if strings.Contains(test.content, "stub-helper") {
				args = append(args, "--binary", "stub-helper")
			}
			code := run(t.Context(), append(args, file), &out, &errOut)
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
