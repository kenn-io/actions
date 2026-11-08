package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"
)

var flagLine = regexp.MustCompile(`(?m)^\s*(?:-([\w]),\s*)?(--[\w-]+)(?:[ =](\S+))? {2,}.*$`)

type usage struct {
	path      []string
	arguments bool
	flags     map[string]bool
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
	u := usage{flags: map[string]bool{}}
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
	for _, match := range flagLine.FindAllStringSubmatch(output, -1) {
		takesValue := match[3] != "" && !strings.Contains(match[3], "[=")
		u.flags[match[2]] = takesValue
		if match[1] != "" {
			u.flags["-"+match[1]] = takesValue
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
	var flags []string
	positional, endFlags := false, false
	for i := 0; i < len(args); i++ {
		word := args[i]
		if word == "--" {
			positional, endFlags = true, true
			continue
		}
		if !endFlags && word != "-" && strings.HasPrefix(word, "-") {
			name, _, assigned := strings.Cut(word, "=")
			attached := len(name) > 2 && name[1] != '-'
			if attached {
				name = name[:2]
			}
			if current.flags[name] && !assigned && !attached && i+1 < len(args) {
				i++
			}
			flags = append(flags, name)
			continue
		}
		if positional {
			continue
		}
		if word == "help" && len(current.path) == 1 {
			continue
		}
		child, err := help(ctx, binary, append(slices.Clone(current.path[1:]), word))
		if err == nil && len(child.path) > len(current.path) {
			current = child
			continue
		}
		if !current.arguments {
			return fmt.Errorf("has no command %q", word)
		}
		positional = true
	}
	for _, name := range flags {
		if _, ok := current.flags[name]; !ok {
			return fmt.Errorf("has no flag %q", name)
		}
	}
	return nil
}
