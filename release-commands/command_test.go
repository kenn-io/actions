package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCommands(t *testing.T) {
	for _, test := range []struct {
		name, source string
		want         [][]string
	}{
		{"alias", "stub group alias <id> --limit <n>", [][]string{{"stub", "group", "alias", "0", "--limit", "0"}}},
		{"absent alternative", "stub group child|nope <id>", [][]string{{"stub", "group", "child", "0"}, {"stub", "group", "nope", "0"}}},
		{"spaced synopsis alternatives", "stub run file [--config cfg | --bogus]", [][]string{{"stub", "run", "file", "--config", "cfg"}, {"stub", "run", "file", "--bogus"}}},
		{"synopsis continuation", "stub group child ID [--bogus]", [][]string{{"stub", "group", "child", "ID", "--bogus"}}},
		{"shell operators", "stub run file | cat && stub nope", [][]string{{"stub", "run", "file"}, {"cat"}, {"stub", "nope"}}},
		{"parse failure", "FOO=1 stub run (", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := commands(test.source)
			if test.want == nil {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.want, got)
			}
		})
	}
}
