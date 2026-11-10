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

var flagLine = regexp.MustCompile(`^(?:-([\w])(?:,\s*|/))?(--[\w-]+)(?:[ =](\S+))?(?: {2,}.*)?$`)

type usage struct {
	output      string
	path        []string
	aliases     []string
	arguments   bool
	hasChildren bool
	flags       *pflag.FlagSet
}

// NUL separates arguments because executable paths and process arguments cannot contain it.
type helpCache map[string]usage

func (cache helpCache) help(ctx context.Context, binary string, path []string) (usage, error) {
	key := strings.Join(append([]string{binary}, path...), "\x00")
	if cached, found := cache[key]; found {
		return cached, nil
	}
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
	u, err := describe(string(data), cache[binary].flags)
	if len(u.path) > len(path)+1 {
		u.path, u.arguments = u.path[:len(path)+1], true
	}
	if err == nil {
		cache[key] = u
	}
	return u, err
}

func describe(output string, root *pflag.FlagSet) (usage, error) {
	u := usage{output: output, flags: pflag.NewFlagSet("release help", pflag.ContinueOnError)}
	u.flags.SetOutput(io.Discard)
	inUsage := false
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
		u.hasChildren = u.hasChildren || child
	}
	u.arguments = u.arguments || !u.hasChildren
	if _, rest, ok := strings.Cut(output, "\nAliases:\n"); ok {
		line, _, _ := strings.Cut(rest, "\n")
		for alias := range strings.SplitSeq(line, ",") {
			u.aliases = append(u.aliases, strings.TrimSpace(alias))
		}
	}
	inFlags := false
	for line := range strings.SplitSeq(output, "\n") {
		if line == "Flags:" || line == "Global Flags:" || line == "Inherited Flags:" {
			inFlags = true
			continue
		}
		if line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			inFlags = false
		}
		flagLines := []string{line}
		rest, compact := strings.CutPrefix(line, "Global: ")
		names, compactNames := strings.CutPrefix(line, "Global Flags: ")
		if compactNames {
			rest, _, _ = strings.Cut(names, " (see ")
			compact = true
		}
		if compact {
			rest, _, _ = strings.Cut(rest, ". Details:")
			flagLines = strings.Split(rest, ",")
		} else if !strings.HasPrefix(line, " ") && (strings.Contains(line, "Flags:") || strings.HasPrefix(line, "Global:")) {
			return u, errors.New("unsupported help: unrecognized flag section")
		}
		if !inFlags && !compact {
			continue
		}
		for _, line := range flagLines {
			shorthand := strings.HasPrefix(line, "  -") && !strings.HasPrefix(line, "  --")
			match := flagLine.FindStringSubmatch(strings.TrimSpace(line))
			if match == nil || !compact && !shorthand && !strings.HasPrefix(line, "      --") {
				continue
			}
			name := strings.TrimPrefix(match[2], "--")
			if compactNames {
				if root == nil || root.Lookup(name) == nil {
					return u, fmt.Errorf("unsupported help: no root definition for --%s", name)
				}
				flag := *root.Lookup(name)
				u.flags.AddFlag(&flag)
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
	// Match Cobra's Find behavior before validating flags against the final page.
	for i := 0; i < len(args); i++ {
		if !current.hasChildren {
			break
		}
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
			if !assigned && (flag == nil || flag.NoOptDefVal == "") && i+1 < len(args) {
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
		child, err := cache.help(ctx, binary, append(slices.Clone(current.path[1:]), word))
		if err != nil {
			return err
		}
		if n := len(current.path); child.output != current.output && len(child.path) == n+1 && slices.Equal(child.path[:n], current.path) && (child.path[n] == word || slices.Contains(child.aliases, word)) {
			current = child
			continue
		}
		if !current.arguments {
			return fmt.Errorf("has no command %q", word)
		}
		break
	}
	// String values avoid type checks; a sentinel permits a flag without a sample.
	return current.flags.Parse(append(slices.Clone(args), ""))
}
