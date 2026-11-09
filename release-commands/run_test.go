package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDescribe(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, output, absent string
		args                 []string
	}{
		{"compact global flags", "Usage:\n  compact run <path>\nGlobal: --json, --project <string>, -q/--quiet. Details: compact --help\n", "", []string{"--json", "--project", "foo", "-q"}},
		{"prose flag absent", "--ghost  prose\n\nUsage:\n  prose [flags]\nFlags:\n  -h, --help  help for prose\n", "ghost", nil},
		{"two space long flag absent", "Usage:\n  prose [flags]\nFlags:\n  --indent-only  description\n  -h, --help  help for prose\n", "indent-only", nil},
		{"wrapped description absent", "Usage:\n  stub group child <id> [flags]\nFlags:\n  -n, --limit int   Limit\n                    - json output details\n                    --description-only  continuation\n", "description-only", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := describe(test.output)
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
		{"absent child", "group absent", `has no command "absent"`},
		{"unknown flag", "run file --bogus", "unknown flag: --bogus"},
		{"child flag before child", "group --limit 50 child ID", ""},
		{"unknown flag before child", "group --bogus child ID", "unknown flag: --bogus"},
		{"missing child after deferred flag", "--local=false group absent", `has no command "absent"`},
		{"unknown clustered shorthand", "group child ID -hZ", "unknown shorthand flag: 'Z'"},
		{"malformed shorthand", "run file -=oops", "unknown shorthand flag: '='"},
		{"parent flag", "group --local child ID", "unknown flag: --local"},
		{"inherited flag", "--config cfg group child ID", ""},
		{"leaf arguments", "plain somefile", ""},
		{"group argument synopsis", "named child", ""},
		{"long assignment", "group --limit=anything child ID", ""},
		{"short attached value", "group -n50 child ID", ""},
		{"terminator", "group -- --bogus nope", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := check(t.Context(), fixtureBinary, strings.Fields(test.arguments))
			if test.finding == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, test.finding)
			}
		})
	}
}
