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

var flagLine = regexp.MustCompile(`^\s*(?:-([\w])(?:,\s*|/))?(--[\w-]+)(?:[ =](\S+))?(?: {2,}.*)?$`)

type usage struct {
	path      []string
	arguments bool
	flags     *pflag.FlagSet
}

func help(ctx context.Context, binary string, path []string) (usage, error) {
	home, err := os.MkdirTemp("", "release-help-*")
	if err != nil {
		return usage{}, err
	}
	defer os.RemoveAll(home)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, append([]string{"help"}, path...)...)
	cmd.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin"}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return usage{}, fmt.Errorf("help %s: %w: %s", strings.Join(path, " "), err, strings.TrimSpace(string(output)))
	}
	return describe(string(output))
}

func describe(output string) (usage, error) {
	u := usage{flags: pflag.NewFlagSet("release help", pflag.ContinueOnError)}
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
			match := flagLine.FindStringSubmatch(line)
			if match == nil {
				if compact || strings.HasPrefix(strings.TrimSpace(line), "-") {
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

func check(ctx context.Context, binary string, args []string) error {
	current, err := help(ctx, binary, nil)
	if err != nil {
		return err
	}
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
			} else {
				flag = current.flags.ShorthandLookup(name[1:2])
			}
			if (strings.HasPrefix(name, "--") || len(name) == 2) && !assigned && (flag == nil || flag.NoOptDefVal == "") && i+1 < len(args) {
				i++
			}
			continue
		}
		if word == "help" && len(current.path) == 1 {
			continue
		}
		child, err := help(ctx, binary, append(slices.Clone(current.path[1:]), word))
		if err != nil && len(child.path) > len(current.path) {
			return err
		}
		if err == nil && len(child.path) > len(current.path) {
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
