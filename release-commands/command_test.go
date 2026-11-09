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
		{"nested host placeholder", "docbank mcp [--transport stdio|http] [--listen <ip:port>] [--allowed-host <host[:port]>]", [][]string{{"docbank", "mcp", "--transport", "stdio", "--listen", "<ip:port>", "--allowed-host", "<host[:port]>"}, {"docbank", "mcp", "--transport", "http", "--listen", "<ip:port>", "--allowed-host", "<host[:port]>"}}},
		{"symbolic variables", `stub group $child`, [][]string{{"stub", "group", "$child"}}},
		{"quoted comment", `stub run '#json|yaml'`, [][]string{{"stub", "run", "#json|yaml"}}},
		{"generic command placeholder", "stub group <command> --local", [][]string{{"stub", "group", "--local"}}},
		{"generic subcommand placeholder", "stub group <subcommand> --local", [][]string{{"stub", "group", "--local"}}},
		{"absent alternative", "stub group child|nope <id>", [][]string{{"stub", "group", "child", "<id>"}, {"stub", "group", "nope", "<id>"}}},
		{"spaced synopsis alternatives", "stub run file [--config cfg | --bogus]", [][]string{{"stub", "run", "file", "--config", "cfg"}, {"stub", "run", "file", "--bogus"}}},
		{"shell operators", "stub run file | cat && stub nope", [][]string{{"stub", "run", "file"}, {"cat"}, {"stub", "nope"}}},
		{"parse failure", "FOO=1 stub run (", nil},
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
