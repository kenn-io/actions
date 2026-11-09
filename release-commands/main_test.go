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
		binaries                    []string
	}{
		{"installer syntax error", ".sh", "echo 'stub run", "cannot read installer messages", 0, []string{"stub"}},
		{"markdown parse escape", ".md", "`stub started (pid 12)`", "add <!-- release-commands: expected-failure -->", 1, []string{"stub"}},
		{"marked missing binary output", ".md", "<!-- release-commands: expected-failure -->\n\n`stub-helper started (pid 12)`", "", 0, []string{"stub", "stub-helper"}},
		{"unknown command", ".md", "Intro\n\n`stub nope`", `v1.0.0 has no command "nope"`, 3, []string{"stub"}},
		{"misconfigured binary", ".md", "`stub nope`", "stbu: not in", 0, []string{"stbu"}},
		{"missing binary", ".md", "`stub-helper run`", "stub-helper: not in", 1, []string{"stub", "stub-helper"}},
		{"marked missing binary", ".md", "Not in the latest release yet\n\n`stub-helper run`", "", 0, []string{"stub", "stub-helper"}},
		{"unreleased", ".md", "Not in the latest release yet\n\n`stub nope`", "", 0, []string{"stub"}},
		{"passing marker", ".md", "Not in the latest release yet\n\n`stub run file`", "passes; remove unreleased note", 3, []string{"stub"}},
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
