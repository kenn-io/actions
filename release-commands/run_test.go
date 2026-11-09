package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func TestDescribe(t *testing.T) {
	t.Parallel()
	rootOutput, err := os.ReadFile("testdata/help/msgvault-root.txt")
	require.NoError(t, err)
	root, err := describe(string(rootOutput), nil)
	require.NoError(t, err)
	for _, test := range []struct {
		name, output, file, absent, finding string
		globals                             *pflag.FlagSet
		path                                []string
		arguments                           bool
		parse                               [][]string
	}{
		{name: "roborev compact global flags", output: "Usage:\n  roborev show [flags]\nGlobal: --server <string>, -v/--verbose. Details: roborev --help\n", path: []string{"roborev", "show"}, arguments: true, parse: [][]string{{"--server", "example", "-v"}}},
		{name: "multiword flag type", output: "Usage:\n  kata federation enroll [flags]\nFlags:\n      --spoke-instance kata federation identity   spoke instance UID\n", path: []string{"kata", "federation", "enroll"}, arguments: true, parse: [][]string{{"--spoke-instance", "example-instance"}}},
		{name: "prose flag absent", output: "      --ghost  prose\n\nUsage:\n  prose [flags]\nFlags:\n  -h, --help  help for prose\n", absent: "ghost", path: []string{"prose"}, arguments: true},
		{name: "wrapped description absent", output: "Usage:\n  stub group child <id> [flags]\nFlags:\n  -n, --limit int   Limit\n                    --description-only  continuation\n", absent: "description-only", path: []string{"stub", "group", "child"}, arguments: true},
		{name: "unsupported flag row", output: "Usage:\n  stub [flags]\nFlags:\n  --quiet   Quiet output\n", finding: "unsupported help"},
		{name: "unsupported flag section", output: "Usage:\n  stub [flags]\nPersistent Flags:\n      --quiet   Quiet output\n", finding: "unsupported help: unrecognized flag section"},
		{name: "msgvault-calendar-create", file: "msgvault-calendar-create", globals: root.flags, path: []string{"msgvault", "calendar", "create"}, arguments: true, parse: [][]string{{"--account", "example-account"}, {"--json"}, {"--home", "example-home"}, {"--local"}, {"--agent-url", "https://example.com"}, {"--verbose"}}},
		{name: "kata", file: "kata", path: []string{"kata"}, parse: [][]string{{"--json"}, {"--format", "agent"}, {"--quiet"}}},
		{name: "roborev", file: "roborev", path: []string{"roborev", "review"}, arguments: true, parse: [][]string{{"--branch"}, {"--server", "example-server"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			output := test.output
			if test.file != "" {
				data, err := os.ReadFile("testdata/help/" + test.file + ".txt")
				require.NoError(t, err)
				output = string(data)
			}
			got, err := describe(output, test.globals)
			if test.finding != "" {
				require.ErrorContains(t, err, test.finding)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.path, got.path)
			require.Equal(t, test.arguments, got.arguments)
			if test.absent != "" {
				require.Nil(t, got.flags.Lookup(test.absent))
			}
			for _, args := range test.parse {
				require.NoError(t, got.flags.Parse(args), args)
				require.Empty(t, got.flags.Args(), args)
			}
		})
	}
}

func TestCheck(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, arguments, finding string }{
		{"disabled flag parsing", "unsafe file --help=false", ""},
		{"help target after terminator", "help -- group nope", `has no command "nope"`},
		{"help known target after terminator", "help -- group child", ""},
		{"help rejects target command flag", "help plain --log.level debug", "unknown flag: --log.level"},
		{"count flag then command", "-v nope", `has no command "nope"`},
		{"lowercase symbolic child", "group $child", ""},
		{"lowercase symbolic argument flag", "plain $path --bogus", "unknown flag: --bogus"},
		{"symbolic child flag", "group <cmd> --child-only", ""},
		{"group positional arguments", "named alice extra", ""},
		{"child help error with positional parent", "named broken", "child help failed"},
		{"unknown flag", "run file --bogus", "unknown flag: --bogus"},
		{"child flag before child", "group --limit 50 child ID", ""},
		{"unknown flag before child", "group --bogus child ID", "unknown flag: --bogus"},
		{"missing child after deferred flag", "--local=false group absent", `has no command "absent"`},
		{"parent flag", "group --local child ID", "unknown flag: --local"},
		{"inherited flag", "--config cfg group child ID", ""},
		{"leaf arguments", "plain somefile", ""},
		{"dotted flag", "plain --log.level debug", ""},
		{"group argument synopsis", "named child", ""},
		{"alias", "group alias ID", ""},
		{"long assignment", "group --limit=anything child ID", ""},
		{"short attached value", "group -n50 child ID", ""},
		{"terminator", "group -- --bogus nope", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := (helpCache{}).check(t.Context(), fixtureBinary, strings.Fields(test.arguments))
			if test.finding == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, test.finding)
			}
		})
	}
}

func TestHelpCache(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	wrapper := fmt.Sprintf("#!/bin/sh\nprintf '%%s %%s\\n' \"${0##*/}\" \"$*\" >> %q\nif [ \"$2\" = malformed ]; then echo unsupported; exit 0; fi\nexec %q \"$@\"\n", log, fixtureBinary)
	for _, name := range []string{"stub", "other"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(wrapper), 0o700))
	}
	cache := helpCache{}
	for _, test := range []struct{ binary, arguments string }{
		{"stub", "group absent"},
		{"stub", "group absent"},
		{"stub", "named broken"},
		{"stub", "named broken"},
		{"stub", "malformed"},
		{"stub", "malformed"},
		{"other", "group absent"},
	} {
		_ = cache.check(t.Context(), filepath.Join(dir, test.binary), strings.Fields(test.arguments))
	}
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
	}, counts)
}

func TestReleasedHelpCache(t *testing.T) {
	dir := t.TempDir()
	helpDir, err := filepath.Abs("testdata/help")
	require.NoError(t, err)
	var wrapper strings.Builder
	wrapper.WriteString("#!/bin/sh\ncase \"$*\" in\n")
	for _, path := range []string{"", "daemon", "daemon stop"} {
		file := "msgvault-" + strings.ReplaceAll(path, " ", "-")
		if path == "" {
			file = "msgvault-root"
		}
		fmt.Fprintf(&wrapper, "%q) cat %q ;;\n", strings.TrimSpace("help "+path), filepath.Join(helpDir, file+".txt"))
	}
	wrapper.WriteString("*) exit 2 ;;\nesac\n")
	binary := filepath.Join(dir, "msgvault")
	require.NoError(t, os.WriteFile(binary, []byte(wrapper.String()), 0o700))
	for _, command := range []string{
		"--home example-home daemon stop",
		"daemon --home example-home stop",
	} {
		t.Run(command, func(t *testing.T) {
			require.NoError(t, (helpCache{}).check(t.Context(), binary, strings.Fields(command)))
		})
	}
}
