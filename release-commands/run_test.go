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
		{"wrapped description absent", "Usage:\n  stub group child <id> [flags]\nFlags:\n  -n, --limit int   Limit\n                    - json output details\n                    --description-only  continuation\n", "description-only", nil, ""},
		{"unsupported flag row", "Usage:\n  stub [flags]\nFlags:\n  --quiet   Quiet output\n", "", nil, "unsupported help"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := describe(test.output)
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
