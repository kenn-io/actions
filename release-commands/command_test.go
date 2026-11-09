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
		{"escaped executable and command", `st\ub pl\ain --bogus`, [][]string{{"stub", "plain", "--bogus"}}},
		{"quoted backslashes", `stub plain 'C:\new' "C:\new" C:\\new`, [][]string{{"stub", "plain", `C:\new`, `C:\new`, `C:\new`}}},
		{"escaped comment whitespace", `stub plain path\ #part --bogus`, [][]string{{"stub", "plain", "path #part", "--bogus"}}},
		{"nested host placeholder", "docbank mcp [--transport stdio|http] [--listen <ip:port>] [--allowed-host <host[:port]>]", [][]string{{"docbank", "mcp", "--transport", "stdio", "--listen", "<ip:port>", "--allowed-host", "<host[:port]>"}, {"docbank", "mcp", "--transport", "http", "--listen", "<ip:port>", "--allowed-host", "<host[:port]>"}}},
		{"multiline comment", "if true; then # note\n  stub nope\nfi", [][]string{{"true"}, {"stub", "nope"}}},
		{"quoted synopsis alternative", `stub run "a b"|c --bogus`, [][]string{{"stub", "run", "a b", "--bogus"}, {"stub", "run", "c", "--bogus"}}},
		{"symbolic variables", `stub group $child`, [][]string{{"stub", "group", "$child"}}},
		{"synopsis comment", "stub group #json|yaml <broken", [][]string{{"stub", "group"}}},
		{"nested quoted hash", `stub run "$(printf "%s" "file #1")" --bogus`, [][]string{{"stub", "run", "0", "--bogus"}, {"printf", "%s", "file #1"}}},
		{"nested quoted synopsis bar", `stub plain "$(grep -E "json|yaml" formats.txt)" --bogus`, [][]string{{"stub", "plain", "0", "--bogus"}, {"grep", "-E", "json|yaml", "formats.txt"}}},
		{"nested substitution synopsis bar", `stub plain "$(printf '%s' $(printf '%s' "x|y") "z|w")" --bogus`, [][]string{{"stub", "plain", "0", "--bogus"}, {"printf", "%s", "0", "z|w"}, {"printf", "%s", "x|y"}}},
		{"quoted comment", `stub run '#json|yaml'`, [][]string{{"stub", "run", "#json|yaml"}}},
		{"generic command placeholder", "stub group <command> --local", [][]string{{"stub", "group", "--local"}}},
		{"generic subcommand placeholder", "stub group <subcommand> --local", [][]string{{"stub", "group", "--local"}}},
		{"absent alternative", "stub group child|nope <id>", [][]string{{"stub", "group", "child", "<id>"}, {"stub", "group", "nope", "<id>"}}},
		{"spaced synopsis alternatives", "stub run file [--config cfg | --bogus]", [][]string{{"stub", "run", "file", "--config", "cfg"}, {"stub", "run", "file", "--bogus"}}},
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
