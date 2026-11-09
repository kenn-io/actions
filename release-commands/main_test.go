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

var fixture, fixtureBinary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "release-fixture-*")
	if err != nil {
		panic(err)
	}
	fixtureBinary = filepath.Join(dir, "stub")
	cmd := exec.CommandContext(context.Background(), "go", "build", "-o", fixtureBinary, "./testdata/stub")
	if output, err := cmd.CombinedOutput(); err != nil {
		panic(fmt.Sprintf("%s: %v", output, err))
	}
	fixture = filepath.Join(dir, "stub_1.0.0_linux_amd64.tar.gz")
	data, err := os.ReadFile(fixtureBinary)
	if err != nil {
		panic(err)
	}
	if err := writeFixtureArchive(fixture, map[string][]byte{"stub": data}); err != nil {
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
		name, ext, content, finding, stdout string
		line                                int
		binaries                            []string
	}{
		{"printf option terminator", ".sh", "echo 'stub plain'\nprintf -- '%s\\n' 'stub nope'", `has no command "nope"`, "", 2, []string{"stub"}},
		{"markdown prose", ".md", "Getting started.", "", "", 0, []string{"stub"}},
		{"installer without commands", ".sh", "echo hello", "no commands for stub", "", 0, []string{"stub"}},
		{"installer parse error names file", ".sh", "echo 'stub nope'\nfoo() { case", "example.sh:2:", "", 2, []string{"stub"}},
		{"passing marked command", ".md", "<!-- release-commands: expected-failure -->\n\n```sh\nstub run file\n```", "", "passes; remove expected-failure marker: stub run file", 4, []string{"stub"}},
		{"trailing comment", ".md", "`stub nope # private remark`", `stub nope: v1.0.0 has no command "nope"`, "", 1, []string{"stub"}},
		{"missing binary", ".md", "`stub-helper run`", "stub-helper: not in", "", 1, []string{"stub", "stub-helper"}},
		{"marked missing binary", ".md", "Not in the latest release yet\n\n`stub-helper run`", "", "", 0, []string{"stub", "stub-helper"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "example"+test.ext)
			require.NoError(t, os.WriteFile(file, []byte(test.content), 0o600))
			var out, errOut bytes.Buffer
			args := []string{"--archive", fixture, "--release", "v1.0.0"}
			for _, binary := range test.binaries {
				args = append(args, "--binary", binary)
			}
			code := run(t.Context(), append(args, file), &out, &errOut)
			if test.finding != "" {
				require.Equal(t, 1, code, errOut.String())
				require.Contains(t, errOut.String(), test.finding)
			} else {
				require.Equal(t, 0, code, errOut.String())
				require.Empty(t, errOut.String())
			}
			if test.stdout != "" {
				require.Equal(t, fmt.Sprintf("%s:%d: %s\n", file, test.line, test.stdout), out.String())
			} else {
				require.Empty(t, out.String())
			}
			if test.line > 0 {
				require.Contains(t, out.String()+errOut.String(), fmt.Sprintf("%s:%d:", file, test.line))
			}
		})
	}
}

func TestReleaseCommandsHelpCache(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	wrapper := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >> %q\nexec %q \"$@\"\n", log, fixtureBinary)
	archive := filepath.Join(dir, "release.tar.gz")
	require.NoError(t, writeFixtureArchive(archive, map[string][]byte{"stub": []byte(wrapper)}))
	first, second := filepath.Join(dir, "first.md"), filepath.Join(dir, "second.md")
	for _, file := range []string{first, second} {
		require.NoError(t, os.WriteFile(file, []byte("`stub run file`"), 0o600))
	}
	var out, errOut bytes.Buffer
	require.Equal(t, 0, run(t.Context(), []string{"--archive", archive, "--binary", "stub", first, second}, &out, &errOut), errOut.String())
	data, err := os.ReadFile(log)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(data), "help run\n"))
}

func TestReleaseCommandsAction(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "-x.md"), []byte("`stub nope`"), 0o600))
	// Supply the local archive through the same release commands CI uses.
	gh := "#!/bin/bash\nset -eu\ncase \"$1 $2\" in\n'release view') echo v1.0.0 ;;\n'release download') cp \"$FIXTURE_ARCHIVE\" \"${@: -1}/stub.tar.gz\" ;;\n*) exit 2 ;;\nesac\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gh"), []byte(gh), 0o700))
	uname := "#!/bin/sh\ncase \"$1\" in\n-s) echo Linux ;;\n-m) echo x86_64 ;;\n*) exit 2 ;;\nesac\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "uname"), []byte(uname), 0o700))
	action, err := os.ReadFile("action.yml")
	require.NoError(t, err)
	_, script, found := strings.Cut(string(action), "      run: |\n")
	require.True(t, found)
	script = strings.ReplaceAll(script, "\n        ", "\n")
	cmd := exec.CommandContext(t.Context(), "bash", "-eu", "-c", script)
	cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "FIXTURE_ARCHIVE="+fixture, "GITHUB_WORKSPACE="+dir, "FILES=-x.md", "BINARIES=stub", "REPOSITORY=example/project", "ASSET=stub.tar.gz")
	output, err := cmd.CombinedOutput()
	require.Error(t, err)
	require.Contains(t, string(output), `-x.md:1: stub nope: v1.0.0 has no command "nope"`)
}
