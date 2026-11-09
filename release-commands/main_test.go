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
		{"version output beside command", ".md", "```text\nstub v0.38.0 (commit abc, built 2026-07-13T15:37:17Z)\nstub nope\n```", `v1.0.0 has no command "nope"`, 3, []string{"stub"}},
		{"missing binary output", ".md", "`stub-helper started (pid 12)`", "stub-helper: not in", 1, []string{"stub", "stub-helper"}},
		{"marked output only", ".md", "<!-- release-commands: expected-failure -->\n\n`stub started (pid 12)`", "", 0, []string{"stub"}},
		{"marked output and command", ".md", "<!-- release-commands: expected-failure -->\n\n```text\nstub started (pid 12)\nstub run file\n```", "passes; remove expected-failure marker", 4, []string{"stub"}},
		{"shell marker hint", ".sh", "echo 'stub nope'", "add # release-commands: expected-failure", 1, []string{"stub"}},
		{"powershell marker hint", ".ps1", "Write-Host 'stub nope'", "add # release-commands: expected-failure", 1, []string{"stub"}},
		{"trailing comment", ".md", "`stub nope # private remark`", `stub nope: v1.0.0 has no command "nope"`, 1, []string{"stub"}},
		{"quoted hash", ".md", "`stub nope '#keep' # remark`", `stub nope '#keep': v1.0.0 has no command "nope"`, 1, []string{"stub"}},
		{"installer prose", ".sh", "echo 'stub started (pid 12)'", "", 0, []string{"stub"}},
		{"installer syntax error", ".sh", "echo 'stub run", "cannot read installer messages", 0, []string{"stub"}},
		{"markdown parse escape", ".md", "`stub started (pid 12)`", "", 0, []string{"stub"}},
		{"command validation escape", ".md", "`stub version 1.2.3`", "add <!-- release-commands: expected-failure -->", 1, []string{"stub"}},
		{"quoted assignment parse failure", ".md", "`MODE=\"a b\" stub run (`", "", 0, []string{"stub"}},
		{"marked assigned missing binary output", ".md", "<!-- release-commands: expected-failure -->\n\n`MODE=\"a b\" stub-helper started (pid 12)`", "", 0, []string{"stub", "stub-helper"}},
		{"powershell generated newline diagnostic", ".ps1", "Write-Host \"Next:`nstub nope\"", `v1.0.0 has no command "nope"`, 1, []string{"stub"}},
		{"marked missing binary output", ".md", "<!-- release-commands: expected-failure -->\n\n`stub-helper started (pid 12)`", "", 0, []string{"stub", "stub-helper"}},
		{"unknown command", ".md", "Intro\n\n`stub nope`", `v1.0.0 has no command "nope"`, 3, []string{"stub"}},
		{"misconfigured binary", ".md", "`stub nope`", "stbu: not in", 0, []string{"stbu"}},
		{"missing binary", ".md", "`stub-helper run`", "stub-helper: not in", 1, []string{"stub", "stub-helper"}},
		{"marked missing binary", ".md", "Not in the latest release yet\n\n`stub-helper run`", "", 0, []string{"stub", "stub-helper"}},
		{"unreleased", ".md", "Not in the latest release yet\n\n`stub nope`", "", 0, []string{"stub"}},
		{"passing marker", ".md", "Not in the latest release yet\n\n`stub run file`", "passes; remove unreleased note", 3, []string{"stub"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			requirePowerShell(t, test.ext)
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
			if test.name == "version output beside command" {
				require.NotContains(t, errOut.String(), file+":2:")
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
	wrapper := fmt.Sprintf("#!/bin/sh\nprintf '%%s %%s\\n' \"${0##*/}\" \"$*\" >> %q\nif [ \"$2\" = malformed ]; then echo unsupported; exit 0; fi\nexec %q \"$@\"\n", log, fixtureBinary)
	archive := filepath.Join(dir, "release.tar.gz")
	require.NoError(t, writeFixtureArchive(archive, map[string][]byte{"stub": []byte(wrapper), "other": []byte(wrapper)}))
	first, second := filepath.Join(dir, "first.md"), filepath.Join(dir, "second.md")
	require.NoError(t, os.WriteFile(first, []byte("`stub group absent`\n`stub group absent`\n`stub named broken`\n`stub named broken`\n`stub malformed`\n`stub malformed`\n`other group absent`"), 0o600))
	require.NoError(t, os.WriteFile(second, []byte("`stub group absent`\n`stub run file`\n`stub run file`"), 0o600))
	for range 2 {
		var out, errOut bytes.Buffer
		require.Equal(t, 1, run(t.Context(), []string{"--archive", archive, "--binary", "stub", "--binary", "other", first, second}, &out, &errOut))
		require.Equal(t, 4, strings.Count(errOut.String(), `has no command "absent"`))
		require.Equal(t, 2, strings.Count(errOut.String(), "child help failed"))
		require.Equal(t, 2, strings.Count(errOut.String(), "unsupported help"))
		data, err := os.ReadFile(log)
		require.NoError(t, err)
		counts := map[string]int{}
		for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
			counts[line]++
		}
		require.Equal(t, map[string]int{
			"stub help": 1, "stub help group": 1, "stub help group absent": 1,
			"stub help named": 1, "stub help named broken": 2, "stub help malformed": 2,
			"other help": 1, "other help group": 1, "other help group absent": 1,
			"stub help run": 1, "stub help run file": 1,
		}, counts)
		require.NoError(t, os.WriteFile(log, nil, 0o600))
	}
}
