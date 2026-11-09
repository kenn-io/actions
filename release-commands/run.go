package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/spf13/pflag"
)

var flagLine = regexp.MustCompile(`^(?:-([\w])(?:,\s*|/))?(--[\w.-]+)(?:[ =](\S+(?: \S+)*))?(?: {2,}.*)?$`)

type usage struct {
	output    string
	path      []string
	aliases   []string
	arguments bool
	flags     *pflag.FlagSet
}

// NUL separates arguments because executable paths and process arguments cannot contain it.
type helpCache map[string]string

func (cache helpCache) help(ctx context.Context, binary string, path []string) (usage, error) {
	key := strings.Join(append([]string{binary}, path...), "\x00")
	output, found := cache[key]
	if !found {
		home, err := os.MkdirTemp("", "release-help-*")
		if err != nil {
			return usage{}, err
		}
		defer os.RemoveAll(home)
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, append([]string{"help"}, path...)...)
		cmd.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin"}
		data, err := cmd.CombinedOutput()
		if err != nil {
			return usage{}, fmt.Errorf("help %s: %w: %s", strings.Join(path, " "), err, strings.TrimSpace(string(data)))
		}
		output = string(data)
	}
	u, err := describe(output)
	if err == nil {
		cache[key] = output
	}
	if len(u.path) > len(path)+1 {
		u.path, u.arguments = u.path[:len(path)+1], true
	}
	return u, err
}

func describe(output string) (usage, error) {
	u := usage{output: output, flags: pflag.NewFlagSet("release help", pflag.ContinueOnError)}
	u.flags.SetOutput(io.Discard)
	inUsage, hasChildren := false, false
	for line := range strings.SplitSeq(output, "\n") {
		if line == "Usage:" {
			inUsage = true
			continue
		}
		if inUsage && line != "" && !strings.HasPrefix(line, " ") {
			inUsage = false
		}
		if !inUsage {
			continue
		}
		var path []string
		child, arguments := false, false
		for word := range strings.FieldsSeq(line) {
			switch {
			case word == "[command]" || word == "<command>" || word == "<subcommand>":
				child = true
			case strings.HasPrefix(word, "[") || placeholder(word):
				arguments = arguments || word != "[flags]"
			default:
				if !child && !arguments {
					path = append(path, word)
				}
			}
		}
		if len(path) > 0 && u.path == nil {
			u.path = path
		}
		u.arguments = u.arguments || (!child && arguments)
		hasChildren = hasChildren || child
	}
	u.arguments = u.arguments || !hasChildren
	if _, rest, ok := strings.Cut(output, "\nAliases:\n"); ok {
		line, _, _ := strings.Cut(rest, "\n")
		for alias := range strings.SplitSeq(line, ",") {
			u.aliases = append(u.aliases, strings.TrimSpace(alias))
		}
	}
	inFlags := false
	for line := range strings.SplitSeq(output, "\n") {
		if line == "Flags:" || line == "Global Flags:" {
			inFlags = true
			continue
		}
		if line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			inFlags = false
		}
		flagLines := []string{line}
		rest, compact := strings.CutPrefix(line, "Global: ")
		if compact {
			rest, _, _ = strings.Cut(rest, ". Details:")
			flagLines = strings.Split(rest, ",")
		}
		if !inFlags && !compact {
			continue
		}
		for _, line := range flagLines {
			shorthand := strings.HasPrefix(line, "  -") && !strings.HasPrefix(line, "  --")
			if !compact && !shorthand && !strings.HasPrefix(line, "      --") {
				continue
			}
			line = strings.TrimSpace(line)
			match := flagLine.FindStringSubmatch(line)
			if match == nil {
				if trimmed := strings.TrimSpace(line); compact || strings.HasPrefix(trimmed, "-") && !strings.HasPrefix(trimmed, "- ") {
					return u, errors.New("unsupported help: unrecognized flag line")
				}
				continue
			}
			name := strings.TrimPrefix(match[2], "--")
			if u.flags.Lookup(name) != nil {
				continue
			}
			u.flags.StringP(name, match[1], "", "")
			if match[3] == "" || strings.Contains(match[3], "[=") {
				u.flags.Lookup(name).NoOptDefVal = "true"
			}
		}
	}
	if len(u.path) == 0 {
		return u, errors.New("unsupported help: no recognizable Usage section")
	}
	return u, nil
}

func (cache helpCache) check(ctx context.Context, binary string, args []string) error {
	current, err := cache.help(ctx, binary, nil)
	if err != nil {
		return err
	}
	unknownFlag := false
	var missing error
	// Match Cobra's Find behavior before validating flags against the final page.
	for i := 0; i < len(args); i++ {
		word := args[i]
		if word == "--" {
			break
		}
		if word != "-" && strings.HasPrefix(word, "-") {
			name, _, assigned := strings.Cut(word, "=")
			var flag *pflag.Flag
			if strings.HasPrefix(name, "--") {
				flag = current.flags.Lookup(name[2:])
			} else if len(name) >= 2 {
				flag = current.flags.ShorthandLookup(name[1:2])
			}
			unknownFlag = unknownFlag || flag == nil
			if (strings.HasPrefix(name, "--") || len(name) == 2) && !assigned && (flag == nil || flag.NoOptDefVal == "") && i+1 < len(args) {
				i++
			}
			continue
		}
		if placeholder(word) {
			if !current.arguments {
				args = args[:i]
			}
			break
		}
		if word == "help" && len(current.path) == 1 {
			continue
		}
		child, err := cache.help(ctx, binary, append(slices.Clone(current.path[1:]), word))
		if err != nil {
			return err
		}
		if n := len(current.path); child.output != current.output && len(child.path) == n+1 && slices.Equal(child.path[:n], current.path) && (child.path[n] == word || slices.Contains(child.aliases, word)) {
			current = child
			continue
		}
		if !current.arguments {
			missing = fmt.Errorf("has no command %q", word)
			if !unknownFlag {
				return missing
			}
		}
		break
	}
	// String values avoid type checks; a sentinel permits a flag without a sample.
	if err := current.flags.Parse(append(slices.Clone(args), "")); err != nil {
		return err
	}
	return missing
}
