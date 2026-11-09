package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDescribe(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, output, absent string
		args                 []string
		finding              string
	}{
		{"compact global flags", "Usage:\n  compact run <path>\nGlobal: --json, --project <string>, -q/--quiet. Details: compact --help\n", "", []string{"--json", "--project", "foo", "-q"}, ""},
		{"multiword flag type", "Usage:\n  kata federation enroll [flags]\nFlags:\n      --spoke-instance kata federation identity   spoke instance UID\n", "", []string{"--spoke-instance", "example-instance"}, ""},
		{"prose flag absent", "      --ghost  prose\n\nUsage:\n  prose [flags]\nFlags:\n  -h, --help  help for prose\n", "ghost", nil, ""},
		{"wrapped description absent", "Usage:\n  stub group child <id> [flags]\nFlags:\n  -n, --limit int   Limit\n                    --description-only  continuation\n", "description-only", nil, ""},
		{"unsupported flag row", "Usage:\n  stub [flags]\nFlags:\n  --quiet   Quiet output\n", "", nil, "unsupported help"},
		{"unsupported flag section", "Usage:\n  stub [flags]\nPersistent Flags:\n      --quiet   Quiet output\n", "", nil, "unsupported help: unrecognized flag section"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := describe(test.output, nil)
			if test.finding != "" {
				require.ErrorContains(t, err, test.finding)
				return
			}
			require.NoError(t, err)
			if test.absent != "" {
				require.Nil(t, got.flags.Lookup(test.absent))
			} else {
				require.NoError(t, got.flags.Parse(test.args))
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
		{"unknown clustered shorthand", "group child ID -hZ", "unknown shorthand flag: 'Z'"},
		{"malformed shorthand", "run -=oops file", "unknown shorthand flag: '='"},
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

func TestReleasedHelp(t *testing.T) {
	rootOutput, err := os.ReadFile("testdata/help/msgvault-root.txt")
	require.NoError(t, err)
	root, err := describe(string(rootOutput), nil)
	require.NoError(t, err)
	for _, test := range []struct {
		file  string
		flags []string
	}{
		{"msgvault-calendar-create", []string{"account", "json", "home", "local", "agent-url", "verbose"}},
		{"kata", []string{"json", "format", "quiet"}},
		{"roborev", []string{"branch", "server"}},
	} {
		t.Run(test.file, func(t *testing.T) {
			output, err := os.ReadFile("testdata/help/" + test.file + ".txt")
			require.NoError(t, err)
			got, err := describe(string(output), root.flags)
			require.NoError(t, err)
			if test.file == "roborev" {
				require.NoError(t, got.flags.Parse([]string{"--branch"}))
				require.NoError(t, got.flags.Parse([]string{"--server", "example-server"}))
			}
			for _, flag := range test.flags {
				require.NotNil(t, got.flags.Lookup(flag), flag)
			}
		})
	}
	t.Run("compact names preserve local overrides", func(t *testing.T) {
		output := "Usage:\n  msgvault test [flags]\nFlags:\n      --home   local override\nGlobal Flags: --home, --agent-url\n"
		got, err := describe(output, root.flags)
		require.NoError(t, err)
		require.ErrorContains(t, got.flags.Parse([]string{"--home", "--bogus"}), "unknown flag: --bogus")
	})
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
