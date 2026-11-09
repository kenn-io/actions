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
		{"synopsis comment", "stub group #json|yaml <broken", [][]string{{"stub", "group"}}},
		{"quoted comment", `stub run '#json|yaml'`, [][]string{{"stub", "run", "#json|yaml"}}},
		{"angle placeholder", "stub completion <shell>", [][]string{{"stub", "completion", "<shell>"}}},
		{"generic command placeholder", "stub group <command> --local", [][]string{{"stub", "group", "--local"}}},
		{"generic subcommand placeholder", "stub group <subcommand> --local", [][]string{{"stub", "group", "--local"}}},
		{"alias", "stub group alias <id> --limit <n>", [][]string{{"stub", "group", "alias", "<id>", "--limit", "<n>"}}},
		{"absent alternative", "stub group child|nope <id>", [][]string{{"stub", "group", "child", "<id>"}, {"stub", "group", "nope", "<id>"}}},
		{"spaced synopsis alternatives", "stub run file [--config cfg | --bogus]", [][]string{{"stub", "run", "file", "--config", "cfg"}, {"stub", "run", "file", "--bogus"}}},
		{"synopsis continuation", "stub group child ID [--bogus]", [][]string{{"stub", "group", "child", "ID", "--bogus"}}},
		{"shell operators", "stub run file | cat && stub nope", [][]string{{"stub", "run", "file"}, {"cat"}, {"stub", "nope"}}},
		{"parse failure", "FOO=1 stub run (", nil},
		{"command substitution", "TOKEN=$(stub nope)", [][]string{{"stub", "nope"}}},
		{"process substitution", "stub completion bash <(stub completion bash)", [][]string{{"stub", "completion", "bash", "0"}, {"stub", "completion", "bash"}}},
		{"cobra placeholders", "stub serve [flags] [command]", [][]string{{"stub", "serve"}}},
		{"quoted ellipsis value", `stub plain --log.level "..." [--no-such-flag] ... …`, [][]string{{"stub", "plain", "--log.level", "...", "--no-such-flag"}}},
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
