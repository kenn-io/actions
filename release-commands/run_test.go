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
		{"multiword flag type", "Usage:\n  kata federation enroll [flags]\nFlags:\n      --spoke-instance kata federation identity   spoke instance UID\n", "", []string{"--spoke-instance", "example-instance"}},
		{"prose flag absent", "      --ghost  prose\n\nUsage:\n  prose [flags]\nFlags:\n  -h, --help  help for prose\n", "ghost", nil},
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
		{"count flag then command", "-v nope", `has no command "nope"`},
		{"lowercase symbolic child", "group $child", ""},
		{"lowercase symbolic argument flag", "plain $path --bogus", "unknown flag: --bogus"},
		{"symbolic child flag", "group <cmd> --child-only", ""},
		{"disabled flag parsing", "unsafe file --help=false", ""},
		{"group positional arguments", "named alice extra", ""},
		{"child help error with positional parent", "named broken", "child help failed"},
		{"absent child", "group absent", `has no command "absent"`},
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
