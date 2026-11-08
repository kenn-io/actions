package main

import (
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
